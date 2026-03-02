"""Base classes for framework-specific STS integration."""

import logging
from typing import Any, Dict, Optional, Union

from ._actor_service import ActorTokenService
from .client import STSClient, STSConfig, TokenType

logger = logging.getLogger(__name__)


class STSIntegrationBase:
    """Base class for framework-specific STS integrations."""

    def __init__(
        self,
        well_known_uri: str,
        service_account_token_path: Optional[str] = None,
        timeout: int = 30,
        verify_ssl: bool = True,
        additional_config: Optional[Dict[str, Any]] = None,
    ):
        """Initialize the STS integration.

        Args:
            well_known_uri: Well-known configuration URI for the STS server
            timeout: Request timeout in seconds
            verify_ssl: Whether to verify SSL certificates
            additional_config: Additional configuration for the specific framework
        """
        self.well_known_uri = well_known_uri
        self.timeout = timeout
        self.verify_ssl = verify_ssl
        self.additional_config = additional_config or {}

        # Initialize STS client
        config = STSConfig(
            well_known_uri=well_known_uri,
            timeout=timeout,
            verify_ssl=verify_ssl,
        )
        self.sts_client = STSClient(config)
        self.access_token = None  # cached access token
        self._actor_token = ActorTokenService(service_account_token_path).get_actor_token()

    async def exchange_token(
        self,
        subject_token: str,
        subject_token_type: TokenType = TokenType.JWT,
        actor_token: Optional[str] = None,
        actor_token_type: Optional[TokenType] = None,
        resource: Optional[Union[str, list]] = None,
        audience: Optional[Union[str, list]] = None,
        scope: Optional[str] = None,
        requested_token_type: Optional[TokenType] = None,
        additional_parameters: Optional[Dict[str, Any]] = None,
    ) -> str:
        """Exchange token using STS.

        Args:
            subject_token: The security token representing the identity
            subject_token_type: Type of the subject token
            actor_token: The security token representing the identity of the acting party
            actor_token_type: Type of the actor token
            resource: The logical name of the target service or resource
            audience: The logical name of the target service or resource
            scope: The scope of the requested token
            requested_token_type: The type of the requested token
            additional_parameters: Additional parameters for the request

        Returns:
            Access token

        Raises:
            TokenExchangeError: If token exchange fails
        """
        try:
            response = await self.sts_client.exchange_token(
                subject_token=subject_token,
                subject_token_type=subject_token_type,
                actor_token=actor_token,
                actor_token_type=actor_token_type,
                resource=resource,
                audience=audience,
                scope=scope,
                requested_token_type=requested_token_type,
                additional_parameters=additional_parameters,
            )
            logger.debug(f"Successfully obtained access token for ADK with length: {len(response.access_token)}")
            return response.access_token
        except Exception as e:
            logger.error(f"Token exchange failed: {e}")
            raise
