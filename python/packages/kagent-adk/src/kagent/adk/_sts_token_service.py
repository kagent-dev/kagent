import logging
from typing import Optional

from kagent.sts import STSClient, STSConfig, TokenType

logger = logging.getLogger(__name__)


class KAgentSTSTokenService:
    """Token service that uses STS (Security Token Service) for token exchange.

    This service can perform both delegation and impersonation token exchanges
    according to RFC 8693 (OAuth 2.0 Token Exchange).
    """

    def __init__(
        self,
        well_known_uri: str,
    ):
        """Initialize the STS token service.

        Args:
            well_known_uri: Well-known configuration URI for the STS server
        """
        # Initialize STS client
        config = STSConfig(
            well_known_uri=well_known_uri,
        )
        self.sts_client = STSClient(config)

    async def exchange_token(
        self,
        subject_token: str,
        subject_token_type: TokenType = TokenType.JWT,
        actor_token: Optional[str] = None,
        actor_token_type: Optional[TokenType] = None,
        requested_token_type: TokenType = TokenType.ACCESS_TOKEN,
    ) -> str:
        """Exchange token using STS."""
        if actor_token:
            logger.debug("making delegation request with actor token")
            # Default actor_token_type to JWT if not provided
            if actor_token_type is None:
                actor_token_type = TokenType.JWT
            response = await self.sts_client.delegate(
                subject_token=subject_token,
                subject_token_type=subject_token_type,
                actor_token=actor_token,
                actor_token_type=actor_token_type,
                requested_token_type=requested_token_type,
            )
        else:
            logger.debug("making impersonation request without actor token")
            response = await self.sts_client.impersonate(
                subject_token=subject_token,
                subject_token_type=subject_token_type,
                requested_token_type=requested_token_type,
            )

        access_token = response.access_token
        return access_token
