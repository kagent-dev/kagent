import asyncio
import logging
from contextlib import asynccontextmanager
from typing import Any, Optional

logger = logging.getLogger(__name__)

SERVICE_ACCOUNT_TOKEN_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/token"


class KAgentServiceAccountService:
    """Service that manages Kubernetes service account information.

    This service reads the service account token from mounted volume
    for use in STS token exchange as the actor token.
    """

    def __init__(self, app_name: str):
        """Initialize the service account service.

        Args:
            app_name: Name of the application (used as service account name)
        """
        self.app_name = app_name
        self.service_account_token = None
        self.update_lock = asyncio.Lock()
        self.update_task = None

    def lifespan(self):
        """Returns an async context manager to start the token update loop"""

        @asynccontextmanager
        async def _lifespan(app: Any):
            await self._update_token_loop()
            yield
            self._drain()

        return _lifespan

    async def _update_token_loop(self) -> None:
        await self._read_service_account_info()
        # Start background refresh task
        self.update_task = asyncio.create_task(self._refresh_info())

    def _drain(self):
        if self.update_task:
            self.update_task.cancel()

    async def _read_service_account_info(self) -> None:
        """Read service account information from mounted volume"""
        try:
            # Read service account token
            self.service_account_token = await self._read_file(SERVICE_ACCOUNT_TOKEN_PATH)
            logger.debug(
                f"Loaded service account info: Name={self.app_name}, Token={'***' if self.service_account_token else 'None'}"
            )

        except Exception as e:
            logger.error(f"Failed to read service account info: {e}")
            logger.error(f"Token path: {SERVICE_ACCOUNT_TOKEN_PATH}")

    async def _read_file(self, file_path: str) -> Optional[str]:
        """Read service account token from a file asynchronously."""
        try:
            return await asyncio.to_thread(self._read_file_sync, file_path)
        except Exception as e:
            logger.error(f"Failed to read file {file_path}: {e}")
            return None

    def _read_file_sync(self, file_path: str) -> str:
        """Read content from a file synchronously."""
        with open(file_path, "r", encoding="utf-8") as f:
            return f.read().strip()

    async def _refresh_info(self):
        """Background task to refresh service account info periodically."""
        while True:
            await asyncio.sleep(60)  # Refresh every minute
            await self._read_service_account_info()

    async def get_actor_jwt(self) -> Optional[str]:
        """Get the service account JWT token for STS delegation.

        Returns:
            JWT token string if available, None otherwise
        """
        async with self.update_lock:
            # If token is not loaded yet, try to load it now
            if not self.service_account_token:
                logger.debug("No service account token available, attempting to load it now...")
                await self._read_service_account_info()

                if not self.service_account_token:
                    logger.warning("Failed to load service account token")
                    return None

            # The service account token from Kubernetes is already a JWT
            # We can use it directly as the actor token
            logger.debug(f"Successfully retrieved service account token (length: {len(self.service_account_token)})")
            return self.service_account_token
