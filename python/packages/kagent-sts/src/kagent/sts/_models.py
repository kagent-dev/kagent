from __future__ import annotations

from enum import Enum
from typing import Any, Dict, List, Optional, Union

from pydantic import BaseModel, Field, field_validator, model_validator


class TokenType(str, Enum):
    """RFC 8693 defined token types."""

    JWT = "urn:ietf:params:oauth:token-type:jwt"
    SAML2 = "urn:ietf:params:oauth:token-type:saml2"
    SAML1 = "urn:ietf:params:oauth:token-type:saml1"
    ID_TOKEN = "urn:ietf:params:oauth:token-type:id_token"
    ACCESS_TOKEN = "urn:ietf:params:oauth:token-type:access_token"


class GrantType(str, Enum):
    """OAuth 2.0 grant types."""

    TOKEN_EXCHANGE = "urn:ietf:params:oauth:grant-type:token-exchange"


class TokenExchangeRequest(BaseModel):
    """RFC 8693 Token Exchange Request model."""

    grant_type: GrantType = GrantType.TOKEN_EXCHANGE
    subject_token: str = Field(
        ...,
        description="The security token representing the identity of the party on behalf of whom the new token is being requested",
    )
    subject_token_type: TokenType = Field(..., description="The type of the subject_token")
    actor_token: Optional[str] = Field(
        None, description="The security token representing the identity of the acting party"
    )
    actor_token_type: Optional[TokenType] = Field(None, description="The type of the actor_token")
    resource: Optional[Union[str, List[str]]] = Field(
        None, description="The logical name of the target service or resource"
    )
    audience: Optional[Union[str, List[str]]] = Field(
        None, description="The logical name of the target service or resource"
    )
    scope: Optional[str] = Field(None, description="The scope of the requested token")
    requested_token_type: Optional[TokenType] = Field(None, description="The type of the requested token")
    additional_parameters: Optional[Dict[str, Any]] = Field(None, description="Additional parameters for the request")

    @model_validator(mode="after")
    def actor_token_type_required_with_actor_token(self):
        if self.actor_token and not self.actor_token_type:
            raise ValueError("actor_token_type is required when actor_token is provided")
        return self

    def is_delegation_request(self) -> bool:
        """Check if this is a delegation request (has actor_token)."""
        return self.actor_token is not None

    def is_impersonation_request(self) -> bool:
        """Check if this is an impersonation request (no actor_token)."""
        return self.actor_token is None


class TokenExchangeResponse(BaseModel):
    """RFC 8693 Token Exchange Response model."""

    access_token: str = Field(..., description="The issued security token")
    issued_token_type: TokenType = Field(..., description="The type of the issued token")
    token_type: str = Field(default="Bearer", description="The type of the access token")
    expires_in: Optional[int] = Field(None, description="The lifetime in seconds of the access token")
    scope: Optional[str] = Field(None, description="The scope of the access token")
    refresh_token: Optional[str] = Field(None, description="Refresh token if applicable")
    additional_parameters: Optional[Dict[str, Any]] = Field(None, description="Additional response parameters")


class TokenExchangeError(BaseModel):
    """RFC 8693 Token Exchange Error model."""

    error: str = Field(..., description="Error code")
    error_description: Optional[str] = Field(None, description="Human-readable error description")
    error_uri: Optional[str] = Field(None, description="URI identifying the error")
    additional_parameters: Optional[Dict[str, Any]] = Field(None, description="Additional error parameters")


class WellKnownConfiguration(BaseModel):
    """OAuth 2.0 Authorization Server Metadata model."""

    issuer: str = Field(..., description="The authorization server's issuer identifier")
    token_endpoint: str = Field(..., description="The token endpoint URL")
    token_endpoint_auth_methods_supported: List[str] = Field(default_factory=list)
    token_endpoint_auth_signing_alg_values_supported: List[str] = Field(default_factory=list)
    additional_parameters: Optional[Dict[str, Any]] = Field(None, description="Additional configuration parameters")
