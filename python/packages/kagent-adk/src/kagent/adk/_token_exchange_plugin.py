"""Token exchange plugin for dynamic bearer token acquisition.

Implements GDCH (Google Distributed Cloud Hosted) token exchange using
service account credentials to obtain short-lived tokens for API access.
"""

import logging
from typing import Optional

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.adk.plugins.base_plugin import BasePlugin

from kagent.adk.types import ModelUnion  # used for type annotation in create_token_source

logger = logging.getLogger(__name__)


class GDCHTokenSource:
    """Token source for GDCH service account token exchange."""

    def __init__(self, service_account_path: str, audience: str, ca_cert_path: str | None = None):
        self._sa_path = service_account_path
        self._audience = audience
        self._ca_cert_path = ca_cert_path
        self._token: str | None = None
        self._expiry: float = 0.0

    async def get_token(self) -> str:
        import time

        now = time.monotonic()
        if self._token and now < self._expiry - 30:  # 30s buffer
            return self._token
        self._token = self._exchange()
        self._expiry = now + 3600  # fallback; overridden if creds.expiry is set
        return self._token

    def _exchange(self) -> str:
        import google.auth
        import requests
        from google.auth.transport import requests as google_requests

        creds, _ = google.auth.load_credentials_from_file(self._sa_path)
        creds = creds.with_gdch_audience(self._audience)
        session = requests.Session()
        if self._ca_cert_path:
            session.verify = True
            session.cert = self._ca_cert_path
        req = google_requests.Request(session=session)
        creds.refresh(req)
        if creds.expiry:
            import datetime
            import time

            self._expiry = (
                time.monotonic() + (creds.expiry - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
            )
        return creds.token


def create_token_source(model_config: ModelUnion) -> Optional[GDCHTokenSource]:
    """Create a token source from model configuration if token exchange is configured.

    Token exchange is only supported for OpenAI-compatible endpoints (e.g., GDCH).
    """
    te = getattr(model_config, "token_exchange", None)
    if te is None:
        return None

    if te.type == "GDCHServiceAccount" and te.gdch_service_account is not None:
        return GDCHTokenSource(
            service_account_path=te.gdch_service_account.service_account_path,
            audience=te.gdch_service_account.audience,
            ca_cert_path=model_config.tls_ca_cert_path,
        )
    return None


class TokenExchangePlugin(BasePlugin):
    """Plugin that exchanges credentials for bearer tokens before model calls."""

    def __init__(self, token_source: GDCHTokenSource):
        super().__init__(name="token_exchange")
        self._token_source = token_source

    async def before_model_callback(
        self, *, callback_context: CallbackContext, llm_request: LlmRequest
    ) -> Optional[LlmResponse]:
        from ._llm_passthrough_plugin import SupportsPassthroughAuth

        model = callback_context._invocation_context.agent.model
        if not isinstance(model, SupportsPassthroughAuth):
            return None
        token = await self._token_source.get_token()
        model.set_passthrough_key(token)
        logger.debug("Set LLM API key from token exchange")
        return None
