from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from kagent.adk._sts_token_service import KAgentSTSTokenService
from kagent.sts import TokenExchangeResponse, TokenType


class TestKAgentSTSTokenService:
    """Test cases for KAgentSTSTokenService."""

    @pytest.fixture
    def sts_token_service(self, mock_sts_config):
        """Create STS token service with mocked dependencies."""
        with patch("kagent.adk._sts_token_service.STSClient") as mock_sts_client:
            service = KAgentSTSTokenService(well_known_uri=mock_sts_config["well_known_uri"])
            service.sts_client = mock_sts_client.return_value
            return service

    @pytest.fixture
    def mock_sts_config(self):
        """Mock STS configuration."""
        return {"well_known_uri": "https://sts.example.com/.well-known/openid_configuration"}

    @pytest.mark.asyncio
    async def test_token_exchange_delegation_success(self, sts_token_service):
        """Test successful token exchange with delegation (subject + actor tokens)."""
        # Mock successful token exchange response
        mock_response = TokenExchangeResponse(
            access_token="delegated-access-token-123",
            issued_token_type="urn:ietf:params:oauth:token-type:access_token",
            token_type="Bearer",
            expires_in=3600,
            scope="test-scope",
        )

        # Mock the delegate method on the existing sts_client
        sts_token_service.sts_client.delegate = AsyncMock(return_value=mock_response)

        # Test token exchange
        result = await sts_token_service.exchange_token(subject_token="subject-token", actor_token="actor-token")

        assert result == "delegated-access-token-123"
        sts_token_service.sts_client.delegate.assert_called_once()

    @pytest.mark.asyncio
    async def test_token_exchange_impersonation_success(self, mock_sts_config):
        """Test successful token exchange with impersonation (subject token only)."""
        # Create service without actor token service for impersonation
        with patch("kagent.adk._sts_token_service.STSClient"):
            service = KAgentSTSTokenService(well_known_uri=mock_sts_config["well_known_uri"])

            # Mock successful token exchange response
            mock_response = TokenExchangeResponse(
                access_token="impersonated-access-token-456",
                issued_token_type="urn:ietf:params:oauth:token-type:access_token",
                token_type="Bearer",
                expires_in=3600,
                scope="test-scope",
            )

            # Mock the impersonate method on the existing sts_client
            service.sts_client.impersonate = AsyncMock(return_value=mock_response)

            # Test token exchange
            result = await service.exchange_token(subject_token="subject-token", actor_token=None)

            assert result == "impersonated-access-token-456"
            service.sts_client.impersonate.assert_called_once()

    @pytest.mark.asyncio
    async def test_token_exchange_failure(self, sts_token_service):
        """Test token exchange failure handling."""
        # Mock token exchange failure
        sts_token_service.sts_client.delegate = AsyncMock(side_effect=Exception("STS server error"))

        # Test token exchange should raise exception
        with pytest.raises(Exception) as exc_info:
            await sts_token_service.exchange_token(subject_token="subject-token", actor_token="actor-token")

        assert "STS server error" in str(exc_info.value)
