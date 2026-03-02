"""Tests for STSIntegrationBase."""

from typing import Any
from unittest.mock import AsyncMock, Mock, patch

import pytest

from agentsts.core import STSIntegrationBase
from agentsts.core.client import TokenType


class MockSTSIntegration(STSIntegrationBase):
    """Concrete implementation for testing."""

    def create_auth_credential(self, access_token: str) -> Any:
        """Create a mock auth credential."""
        return {"access_token": access_token, "type": "mock"}


class TestSTSIntegrationBase:
    """Test cases for STSIntegrationBase."""

    @pytest.mark.asyncio
    async def test_exchange_token_success(self):
        """Test successful token exchange."""
        well_known_uri = "https://sts.example.com/.well-known/openid_configuration"
        subject_token = "subject-token-123"
        expected_access_token = "access-token-456"

        mock_response = Mock()
        mock_response.access_token = expected_access_token

        with patch("agentsts.core._base.STSClient") as mock_sts_client_class:
            mock_sts_client = Mock()
            mock_sts_client.exchange_token = AsyncMock(return_value=mock_response)
            mock_sts_client_class.return_value = mock_sts_client

            with patch("agentsts.core._base.ActorTokenService"):
                integration = MockSTSIntegration(well_known_uri)

                result = await integration.exchange_token(subject_token)

                assert result == expected_access_token
                mock_sts_client.exchange_token.assert_called_once_with(
                    subject_token=subject_token,
                    subject_token_type=TokenType.JWT,
                    actor_token=None,
                    actor_token_type=None,
                    resource=None,
                    audience=None,
                    scope=None,
                    requested_token_type=None,
                    additional_parameters=None,
                )

    @pytest.mark.asyncio
    async def test_exchange_token_failure(self):
        """Test token exchange failure."""
        well_known_uri = "https://sts.example.com/.well-known/openid_configuration"
        subject_token = "invalid-token"

        with patch("agentsts.core._base.STSClient") as mock_sts_client_class:
            mock_sts_client = Mock()
            mock_sts_client.exchange_token = AsyncMock(side_effect=Exception("Token exchange failed"))
            mock_sts_client_class.return_value = mock_sts_client

            with patch("agentsts.core._base.ActorTokenService"):
                integration = MockSTSIntegration(well_known_uri)

                with pytest.raises(Exception, match="Token exchange failed"):
                    await integration.exchange_token(subject_token)

    def test_concrete_implementation(self):
        """Test that concrete implementation works correctly."""
        well_known_uri = "https://sts.example.com/.well-known/openid_configuration"

        with patch("agentsts.core._base.STSClient"):
            with patch("agentsts.core._base.ActorTokenService"):
                integration = MockSTSIntegration(well_known_uri)

                # Test that create_auth_credential works
                access_token = "test-access-token"
                credential = integration.create_auth_credential(access_token)

                assert credential == {"access_token": access_token, "type": "mock"}
