import json
from typing import Any, Dict

import httpx
from pydantic import ValidationError

from ._exceptions import ConfigurationError, NetworkError
from ._exceptions import TokenExchangeError as TokenExchangeException
from ._models import WellKnownConfiguration

# Protocol constants
HTTP_PROTOCOL = "http://"
HTTPS_PROTOCOL = "https://"


async def fetch_well_known_configuration(
    well_known_uri: str, timeout: int = 5, verify_ssl: bool = True
) -> WellKnownConfiguration:
    try:
        async with httpx.AsyncClient(timeout=timeout, verify=verify_ssl) as client:
            response = await client.get(well_known_uri)
            response.raise_for_status()

            data = response.json()

            # add protocol to token_endpoint if it's missing
            if "token_endpoint" in data and not data["token_endpoint"].startswith((HTTP_PROTOCOL, HTTPS_PROTOCOL)):
                # use the protocol from the well_known_uri
                if well_known_uri.startswith(HTTPS_PROTOCOL):
                    protocol = HTTPS_PROTOCOL
                else:
                    protocol = HTTP_PROTOCOL
                data["token_endpoint"] = protocol + data["token_endpoint"]

            config = WellKnownConfiguration.model_validate(data)
            return config

    except httpx.HTTPStatusError as e:
        raise NetworkError(f"Failed to fetch well-known configuration: HTTP {e.response.status_code}") from e
    except httpx.RequestError as e:
        raise NetworkError(f"Network error fetching well-known configuration: {e}") from e
    except (json.JSONDecodeError, ValidationError) as e:
        raise ConfigurationError(f"Invalid well-known configuration response: {e}") from e


def parse_token_exchange_error(response_data: Dict[str, Any]) -> TokenExchangeException:
    """Parse token exchange error response."""
    return TokenExchangeException(
        error=response_data.get("error", "unknown_error"),
        error_description=response_data.get("error_description"),
    )


def extract_jwt_claims(token: str) -> Dict[str, Any]:
    """Extract claims from a JWT token without verification."""
    try:
        import jwt

        # Decode without verification to extract claims
        return jwt.decode(token, options={"verify_signature": False})
    except Exception as e:
        raise ValueError(f"Failed to extract JWT claims: {e}") from e
