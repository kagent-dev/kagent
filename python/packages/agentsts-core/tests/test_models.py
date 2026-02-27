"""Tests for kagent-sts models."""

import pytest
from pydantic import ValidationError

from agentsts.core.client import (
    GrantType,
    TokenExchangeError,
    TokenExchangeRequest,
    TokenExchangeResponse,
    TokenType,
    WellKnownConfiguration,
)


class TestTokenExchangeRequest:
    """Test TokenExchangeRequest model."""

    def test_impersonation_request(self):
        """Test impersonation request creation."""
        request = TokenExchangeRequest(subject_token="test_token", subject_token_type=TokenType.JWT)

        assert request.grant_type == GrantType.TOKEN_EXCHANGE
        assert request.subject_token == "test_token"
        assert request.subject_token_type == TokenType.JWT
        assert request.actor_token is None
        assert request.actor_token_type is None
        assert request.is_impersonation_request() is True
        assert request.is_delegation_request() is False

    def test_delegation_request(self):
        """Test delegation request creation."""
        request = TokenExchangeRequest(
            subject_token="test_token",
            subject_token_type=TokenType.JWT,
            actor_token="actor_token",
            actor_token_type=TokenType.JWT,
        )

        assert request.actor_token == "actor_token"
        assert request.actor_token_type == TokenType.JWT
        assert request.is_delegation_request() is True
        assert request.is_impersonation_request() is False

    def test_actor_token_type_required_with_actor_token(self):
        """Test that actor_token_type is required when actor_token is provided."""
        with pytest.raises(ValidationError) as exc_info:
            TokenExchangeRequest(
                subject_token="test_token",
                subject_token_type=TokenType.JWT,
                actor_token="actor_token",
                # Missing actor_token_type
            )

        # Check that the error message is in the validation error
        error_messages = []
        for error in exc_info.value.errors():
            error_messages.append(error["msg"])

        assert any("actor_token_type is required when actor_token is provided" in msg for msg in error_messages)

    def test_optional_parameters(self):
        """Test optional parameters."""
        request = TokenExchangeRequest(
            subject_token="test_token",
            subject_token_type=TokenType.JWT,
            resource="https://api.example.com",
            audience="https://api.example.com",
            scope="read write",
            requested_token_type=TokenType.JWT,
        )

        assert request.resource == "https://api.example.com"
        assert request.audience == "https://api.example.com"
        assert request.scope == "read write"
        assert request.requested_token_type == TokenType.JWT


class TestTokenExchangeResponse:
    """Test TokenExchangeResponse model."""

    def test_successful_response(self):
        """Test successful response creation."""
        response = TokenExchangeResponse(
            access_token="new_token",
            issued_token_type=TokenType.JWT,
            token_type="Bearer",
            expires_in=3600,
            scope="read write",
        )

        assert response.access_token == "new_token"
        assert response.issued_token_type == TokenType.JWT
        assert response.token_type == "Bearer"
        assert response.expires_in == 3600
        assert response.scope == "read write"

    def test_minimal_response(self):
        """Test minimal response creation."""
        response = TokenExchangeResponse(access_token="new_token", issued_token_type=TokenType.JWT)

        assert response.access_token == "new_token"
        assert response.issued_token_type == TokenType.JWT
        assert response.token_type == "Bearer"  # Default value
        assert response.expires_in is None
        assert response.scope is None


class TestTokenExchangeError:
    """Test TokenExchangeError model."""

    def test_error_creation(self):
        """Test error creation."""
        error = TokenExchangeError(
            error="invalid_request", error_description="The request is missing a required parameter"
        )

        assert error.error == "invalid_request"
        assert error.error_description == "The request is missing a required parameter"

    def test_minimal_error(self):
        """Test minimal error creation."""
        error = TokenExchangeError(error="server_error")

        assert error.error == "server_error"
        assert error.error_description is None


class TestWellKnownConfiguration:
    """Test WellKnownConfiguration model."""

    def test_configuration_creation(self):
        """Test configuration creation."""
        config = WellKnownConfiguration(
            issuer="https://auth.example.com",
            token_endpoint="https://auth.example.com/oauth/token",
            token_endpoint_auth_methods_supported=["client_secret_basic", "client_secret_post"],
            token_endpoint_auth_signing_alg_values_supported=["RS256", "HS256"],
        )

        assert config.issuer == "https://auth.example.com"
        assert config.token_endpoint == "https://auth.example.com/oauth/token"
        assert "client_secret_basic" in config.token_endpoint_auth_methods_supported
        assert "RS256" in config.token_endpoint_auth_signing_alg_values_supported

    def test_minimal_configuration(self):
        """Test minimal configuration creation."""
        config = WellKnownConfiguration(
            issuer="https://auth.example.com", token_endpoint="https://auth.example.com/oauth/token"
        )

        assert config.issuer == "https://auth.example.com"
        assert config.token_endpoint == "https://auth.example.com/oauth/token"
        assert config.token_endpoint_auth_methods_supported == []
        assert config.token_endpoint_auth_signing_alg_values_supported == []
