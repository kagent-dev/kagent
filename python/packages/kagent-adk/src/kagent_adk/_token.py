import logging  # noqa: I001
import asyncio
from contextlib import asynccontextmanager
from typing import Any, Optional

KAGENT_TOKEN_PATH = "/var/run/secrets/tokens/kagent-token"
logger = logging.getLogger(__name__)


class KAgentTokenService:
    """Reads a k8s token from a file, and reloads it
    periodically.
    """

    def __init__(self, app_name: str):
        self.token = None
        self.update_lock = asyncio.Lock()
        self.update_task = None
        self.app_name = app_name

    def lifespan(self):
        """Returns an async context manager to start the token update loop"""

        @asynccontextmanager
        async def _lifespan(app: Any):
            await self._update_token_loop()
            yield
            self._drain()

        return _lifespan

    def event_hooks(self):
        """Returns a dictionary of event hooks for the application
        to use when creating the httpx.AsyncClient.
        """
        return {"request": [self._add_bearer_token]}

    async def _update_token_loop(self) -> None:
        self.token = await self._read_kagent_token()
        # keep it updated - launch a background task to refresh it periodically
        self.update_task = asyncio.create_task(self._refresh_token())

    def _drain(self):
        if self.update_task:
            self.update_task.cancel()

    async def _get_token(self) -> str | None:
        async with self.update_lock:
            return self.token

    async def _read_kagent_token(self) -> str | None:
        return await asyncio.to_thread(read_token)

    async def _refresh_token(self):
        while True:
            await asyncio.sleep(60)  # Wait for 60 seconds before refreshing
            token = await self._read_kagent_token()
            if token is not None and token != self.token:
                async with self.update_lock:
                    self.token = token

    async def _add_bearer_token(self, request):
        # Your function to generate headers dynamically
        token = await self._get_token()
        headers = {"X-Agent-Name": self.app_name}
        if token:
            headers["Authorization"] = f"Bearer {token}"
        request.headers.update(headers)


def read_token() -> str | None:
    try:
        with open(KAGENT_TOKEN_PATH, "r", encoding="utf-8") as f:
            token = f.read()
            return token.strip()
    except OSError as e:
        logger.error(f"Error reading token from {KAGENT_TOKEN_PATH}: {e}")
        return None
