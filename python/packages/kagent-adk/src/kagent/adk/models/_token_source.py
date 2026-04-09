"""Token source implementations for dynamic bearer token acquisition."""

import logging
import time

logger = logging.getLogger(__name__)


class GDCHTokenSource:
    """Exchanges a GDCH service account JSON for short-lived bearer tokens.

    Tokens are cached and refreshed automatically with a 30-second buffer
    before expiry. The CA certificate path is used for custom TLS verification
    when connecting to the GDCH token endpoint.
    """

    def __init__(self, service_account_path: str, audience: str, ca_cert_path: str | None = None) -> None:
        self._sa_path = service_account_path
        self._audience = audience
        self._ca_cert_path = ca_cert_path
        self._token: str | None = None
        self._expiry: float = 0.0

    async def get_token(self) -> str:
        now = time.monotonic()
        if self._token and now < self._expiry - 30:  # 30 s buffer
            return self._token
        self._token = self._exchange()
        self._expiry = now + 3600  # fallback; overridden below if creds carry expiry
        return self._token

    def _exchange(self) -> str:
        import datetime

        import google.auth
        import requests
        from google.auth.transport import requests as google_requests

        creds, _ = google.auth.load_credentials_from_file(self._sa_path)
        creds = creds.with_gdch_audience(self._audience)
        session = requests.Session()
        if self._ca_cert_path:
            # session.verify accepts a CA bundle path to verify the server's TLS
            # certificate against a custom CA (not a client cert — that's session.cert).
            session.verify = self._ca_cert_path
        creds.refresh(google_requests.Request(session=session))
        if creds.expiry:
            self._expiry = (
                time.monotonic() + (creds.expiry - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
            )
        return creds.token
