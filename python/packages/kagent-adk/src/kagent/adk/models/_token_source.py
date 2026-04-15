"""Token source implementations for dynamic bearer token acquisition."""

import asyncio
import datetime
import logging
import time

logger = logging.getLogger(__name__)


class GDCHTokenSource:
    """Exchanges a GDCH service account JSON for short-lived bearer tokens.

    Tokens are cached and refreshed automatically with a 30-second buffer
    before expiry. The CA certificate path is used for custom TLS verification
    when connecting to the GDCH token endpoint.
    """

    def __init__(
        self,
        service_account_path: str,
        audience: str,
        ca_cert_path: str | None = None,
        tls_disable_verify: bool = False,
    ) -> None:
        self._sa_path = service_account_path
        self._audience = audience
        self._ca_cert_path = ca_cert_path
        self._tls_disable_verify = tls_disable_verify
        self._token: str | None = None
        self._expiry: float = 0.0
        self._lock = asyncio.Lock()

    async def get_token(self) -> str:
        now = time.monotonic()
        if self._token and now < self._expiry - 30:  # 30 s buffer
            return self._token
        async with self._lock:
            # Re-check after acquiring the lock (another coroutine may have refreshed).
            now = time.monotonic()
            if self._token and now < self._expiry - 30:
                return self._token
            self._expiry = now + 3600  # fallback when creds do not expose expiry
            self._token = await asyncio.to_thread(self._exchange)
            return self._token

    def _exchange(self) -> str:
        import google.auth
        import requests
        from google.auth.transport import requests as google_requests

        creds, _ = google.auth.load_credentials_from_file(self._sa_path)
        creds = creds.with_gdch_audience(self._audience)
        session = requests.Session()
        if self._tls_disable_verify:
            session.verify = False
        elif self._ca_cert_path:
            # Replaces the REQUESTS_CA_BUNDLE environment variable
            session.verify = self._ca_cert_path
        creds.refresh(google_requests.Request(session=session))
        if creds.expiry:
            expiry = creds.expiry
            # If the expiry is not timezone-aware, set it to UTC
            if expiry.tzinfo is None:
                expiry = expiry.replace(tzinfo=datetime.timezone.utc)
            self._expiry = (
                time.monotonic() + (expiry - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
            )
        return creds.token
