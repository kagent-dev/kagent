"""Base actor token service for STS integration."""

import logging
from typing import Optional

logger = logging.getLogger(__name__)

SERVICE_ACCOUNT_TOKEN_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/token"


class ActorTokenService:
    """Service that loads actor tokens for STS delegation.

    This service provides a simple, synchronous approach for loading actor tokens
    (like Kubernetes service account tokens) used in STS token exchange.
    """

    def __init__(self, token_path: Optional[str] = None):
        """Initialize the actor token service.

        Args:
            token_path: Path to the token file. Defaults to Kubernetes service account token path.
        """
        self.token_path = token_path or SERVICE_ACCOUNT_TOKEN_PATH

    def get_actor_token(self) -> Optional[str]:
        """Get the actor token for STS delegation.

        This method reads the token from the file each time it's called.
        If loading fails, it returns None.

        Returns:
            Actor token string if available, None otherwise
        """
        try:
            logger.debug(f"Loading actor token from {self.token_path}")

            with open(self.token_path, "r", encoding="utf-8") as f:
                token = f.read().strip()

            if token:
                logger.info("Successfully loaded actor token'")
                return token
            else:
                logger.warning(f"No actor token found at {self.token_path}")
                return None

        except Exception as e:
            logger.error(f"Failed to load actor token': {e}")
            logger.error(f"Token path: {self.token_path}")
            return None
