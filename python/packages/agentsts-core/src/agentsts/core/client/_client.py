import logging
from typing import Any, Dict, Optional, Union

import httpx

from ._config import STSConfig
from ._exceptions import AuthenticationError, NetworkError, TokenExchangeError
from ._models import TokenExchangeRequest, TokenExchangeResponse, TokenType, WellKnownConfiguration
from ._utils import fetch_well_known_configuration, parse_token_exchange_error

logger = logging.getLogger(__name__)


class STSClient:
    """Security Token Service client implementing RFC 8693 OAuth 2.0 Token Exchange."""

    def __init__(
        self,
        config: STSConfig,
    ):
        """
        Initialize STS client.

        Args:
            config: STS configuration
        """
        self.config = config
        self._well_known_config: Optional[WellKnownConfiguration] = None
        self._http_client: Optional[httpx.AsyncClient] = None

    async def __aenter__(self):
        """Async context manager entry."""
        await self._initialize()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        """Async context manager exit."""
        await self.close()

    async def _initialize(self):
        """Initialize the client by fetching well-known configuration."""
        if not self._well_known_config:
            self._well_known_config = await fetch_well_known_configuration(
                self.config.well_known_uri, self.config.timeout, self.config.verify_ssl
            )

        if not self._http_client:
            self._http_client = httpx.AsyncClient(timeout=self.config.timeout, verify=self.config.verify_ssl)

    async def close(self):
        """Close the HTTP client."""
        if self._http_client:
            await self._http_client.aclose()
            self._http_client = None

    def _build_request_data(self, request: TokenExchangeRequest) -> Dict[str, Any]:
        """Build form data for the token exchange request."""
        data = {
            "grant_type": request.grant_type.value,
            "subject_token": request.subject_token,
            "subject_token_type": request.subject_token_type.value,
        }

        # Add actor token for delegation requests
        if request.actor_token:
            data["actor_token"] = request.actor_token
            data["actor_token_type"] = request.actor_token_type.value

        # Add optional parameters
        if request.resource:
            data["resource"] = request.resource
        if request.audience:
            data["audience"] = request.audience
        if request.scope:
            data["scope"] = request.scope
        if request.requested_token_type:
            data["requested_token_type"] = request.requested_token_type.value

        # Add additional parameters
        if request.additional_parameters:
            data.update(request.additional_parameters)

        return data

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
    ) -> TokenExchangeResponse:
        """
        Exchange a token using RFC 8693 OAuth 2.0 Token Exchange.

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
            TokenExchangeResponse containing the issued token

        Raises:
            TokenExchangeError: If token exchange fails
            NetworkError: If network operation fails
        """
        await self._initialize()

        # Build the request
        request = TokenExchangeRequest(
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

        # Prepare the request
        data = self._build_request_data(request)

        try:
            response = await self._http_client.post(self._well_known_config.token_endpoint, data=data)

            if response.status_code == 200:
                response_data = response.json()
                result = TokenExchangeResponse.model_validate(response_data)
                return result
            else:
                # Parse error response
                try:
                    response_data = response.json()
                    error = parse_token_exchange_error(response_data)
                    raise TokenExchangeError(
                        error=error.error, error_description=error.error_description, status_code=response.status_code
                    )
                except (ValueError, KeyError, TypeError) as e:
                    response_text = response.text
                    raise TokenExchangeError(
                        error="invalid_response",
                        error_description=f"Invalid error response: {response_text}",
                        status_code=response.status_code,
                    ) from e

        except httpx.RequestError as e:
            raise NetworkError(f"Network error during token exchange: {e}") from e

    async def impersonate(
        self, subject_token: str, subject_token_type: TokenType = TokenType.JWT, **kwargs
    ) -> TokenExchangeResponse:
        """
        Perform impersonation token exchange (no actor token).

        Args:
            subject_token: The security token representing the identity to impersonate
            subject_token_type: Type of the subject token
            **kwargs: Additional parameters for the token exchange

        Returns:
            TokenExchangeResponse containing the issued token
        """
        try:
            result = await self.exchange_token(
                subject_token=subject_token, subject_token_type=subject_token_type, **kwargs
            )
            return result
        except Exception as e:
            logger.error(f"Exception in impersonate method: {type(e)} - {e}")
            logger.error(f"Exception args: {e.args}")
            raise

    async def delegate(
        self, subject_token: str, subject_token_type: TokenType, actor_token: str, actor_token_type: TokenType, **kwargs
    ) -> TokenExchangeResponse:
        """
        Perform delegation token exchange (with actor token).

        Args:
            subject_token: The security token representing the identity to delegate
            subject_token_type: Type of the subject token
            actor_token: The security token representing the identity of the acting party
            actor_token_type: Type of the actor token
            **kwargs: Additional parameters for the token exchange

        Returns:
            TokenExchangeResponse containing the issued token
        """
        if not subject_token:
            raise AuthenticationError("Subject token required for delegation")

        try:
            result = await self.exchange_token(
                subject_token=subject_token,
                subject_token_type=subject_token_type,
                actor_token=actor_token,
                actor_token_type=actor_token_type,
                **kwargs,
            )
            return result
        except Exception as e:
            logger.error(f"Exception in delegate method: {type(e)} - {e}")
            logger.error(f"Exception args: {e.args}")
            raise
