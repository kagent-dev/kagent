from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest

from agentsts.core.client import (
    AuthenticationError,
    NetworkError,
    STSClient,
    STSConfig,
    TokenExchangeError,
    TokenType,
)


class MockWellKnownConfig:
    """Mock well-known configuration for testing."""

    def __init__(self):
        self.issuer = "https://auth.example.com"
        self.token_endpoint = "https://auth.example.com/oauth/token"
        self.token_endpoint_auth_methods_supported = ["client_secret_basic"]
        self.token_endpoint_auth_signing_alg_values_supported = ["RS256"]


@pytest.fixture
def config():
    """Create test configuration."""
    return STSConfig(
        well_known_uri="https://auth.example.com/.well-known/oauth-authorization-server",
    )


@pytest.fixture
def mock_well_known_config():
    """Create mock well-known configuration."""
    return MockWellKnownConfig()


@pytest.mark.asyncio
async def test_impersonation_token_exchange(config, mock_well_known_config):
    """Test impersonation token exchange."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:
            # Mock successful response
            with patch.object(client._http_client, "post", new_callable=AsyncMock) as mock_post:
                mock_response = MagicMock()
                mock_response.status_code = 200
                mock_response.json.return_value = {
                    "access_token": "eyJhbGciOiJFUzI1NiIsImtpZCI6IjcyIn0.eyJhdWQiOiJ1cm46ZXhhbXBsZTpjb29wZXJhdGlvbi1jb250ZXh0IiwiaXNzIjoiaHR0cHM6Ly9hcy5leGFtcGxlLmNvbSIsImV4cCI6MTQ0MTkxMzYxMCwic2NvcGUiOiJzdGF0dXMgZmVlZCIsInN1YiI6InVzZXJAZXhhbXBsZS5uZXQifQ.3paKl9UySKYB5ng6_cUtQ2qlO8Rc_y7Mea7IwEXTcYbNdwG9-G1EKCFe5fW3H0hwX-MSZ49Wpcb1SiAZaOQBtw",
                    "issued_token_type": "urn:ietf:params:oauth:token-type:jwt",
                    "token_type": "Bearer",
                    "expires_in": 3600,
                }
                mock_post.return_value = mock_response

                response = await client.impersonate("subject_token")

                assert (
                    response.access_token
                    == "eyJhbGciOiJFUzI1NiIsImtpZCI6IjcyIn0.eyJhdWQiOiJ1cm46ZXhhbXBsZTpjb29wZXJhdGlvbi1jb250ZXh0IiwiaXNzIjoiaHR0cHM6Ly9hcy5leGFtcGxlLmNvbSIsImV4cCI6MTQ0MTkxMzYxMCwic2NvcGUiOiJzdGF0dXMgZmVlZCIsInN1YiI6InVzZXJAZXhhbXBsZS5uZXQifQ.3paKl9UySKYB5ng6_cUtQ2qlO8Rc_y7Mea7IwEXTcYbNdwG9-G1EKCFe5fW3H0hwX-MSZ49Wpcb1SiAZaOQBtw"
                )
                assert response.issued_token_type == TokenType.JWT
                assert response.token_type == "Bearer"
                assert response.expires_in == 3600


@pytest.mark.asyncio
async def test_delegation_token_exchange(config, mock_well_known_config):
    """Test delegation token exchange."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:
            # Mock successful response
            with patch.object(client._http_client, "post", new_callable=AsyncMock) as mock_post:
                mock_response = MagicMock()
                mock_response.status_code = 200
                mock_response.json.return_value = {
                    "access_token": "eyJhbGciOiJFUzI1NiIsImtpZCI6IjcyIn0.eyJhdWQiOiJ1cm46ZXhhbXBsZTpjb29wZXJhdGlvbi1jb250ZXh0IiwiaXNzIjoiaHR0cHM6Ly9hcy5leGFtcGxlLmNvbSIsImV4cCI6MTQ0MTkxMzYxMCwic2NvcGUiOiJzdGF0dXMgZmVlZCIsInN1YiI6InVzZXJAZXhhbXBsZS5uZXQiLCJhY3QiOnsic3ViIjoiYWRtaW5AZXhhbXBsZS5uZXQifX0.3paKl9UySKYB5ng6_cUtQ2qlO8Rc_y7Mea7IwEXTcYbNdwG9-G1EKCFe5fW3H0hwX-MSZ49Wpcb1SiAZaOQBtw",
                    "issued_token_type": "urn:ietf:params:oauth:token-type:jwt",
                    "token_type": "N_A",
                    "expires_in": 3600,
                }
                mock_post.return_value = mock_response

                response = await client.delegate(
                    subject_token="subject_token",
                    subject_token_type=TokenType.JWT,
                    actor_token="actor_token",
                    actor_token_type=TokenType.JWT,
                )

                assert (
                    response.access_token
                    == "eyJhbGciOiJFUzI1NiIsImtpZCI6IjcyIn0.eyJhdWQiOiJ1cm46ZXhhbXBsZTpjb29wZXJhdGlvbi1jb250ZXh0IiwiaXNzIjoiaHR0cHM6Ly9hcy5leGFtcGxlLmNvbSIsImV4cCI6MTQ0MTkxMzYxMCwic2NvcGUiOiJzdGF0dXMgZmVlZCIsInN1YiI6InVzZXJAZXhhbXBsZS5uZXQiLCJhY3QiOnsic3ViIjoiYWRtaW5AZXhhbXBsZS5uZXQifX0.3paKl9UySKYB5ng6_cUtQ2qlO8Rc_y7Mea7IwEXTcYbNdwG9-G1EKCFe5fW3H0hwX-MSZ49Wpcb1SiAZaOQBtw"
                )
                assert response.issued_token_type == TokenType.JWT
                assert response.token_type == "N_A"
                assert response.expires_in == 3600


@pytest.mark.asyncio
async def test_delegation_without_subject_token(config, mock_well_known_config):
    """Test delegation without identity token raises error."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:  # No identity token
            with pytest.raises(AuthenticationError) as exc_info:
                await client.delegate(
                    subject_token=None,
                    subject_token_type=TokenType.JWT,
                    actor_token="actor_token",
                    actor_token_type=TokenType.JWT,
                )

                assert "Subject token required for delegation" in str(exc_info.value)


@pytest.mark.asyncio
async def test_token_exchange_error_response(config, mock_well_known_config):
    """Test token exchange error response handling."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:
            with patch.object(client._http_client, "post", new_callable=AsyncMock) as mock_post:
                mock_response = MagicMock()
                mock_response.status_code = 400
                mock_response.json.return_value = {
                    "error": "invalid_request",
                    "error_description": "The request is missing a required parameter",
                }
                mock_response.text = "Invalid error response"
                mock_post.return_value = mock_response

                with pytest.raises(TokenExchangeError) as exc_info:
                    await client.impersonate("subject_token")

                assert exc_info.value.error == "invalid_request"
                assert exc_info.value.error_description == "The request is missing a required parameter"
                assert exc_info.value.status_code == 400


@pytest.mark.asyncio
async def test_network_error(config, mock_well_known_config):
    """Test network error handling."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:
            with patch.object(client._http_client, "post", new_callable=AsyncMock) as mock_post:
                mock_post.side_effect = httpx.RequestError("Network error")

                with pytest.raises(NetworkError) as exc_info:
                    await client.impersonate("subject_token")

                assert "Network error during token exchange" in str(exc_info.value)


@pytest.mark.asyncio
async def test_request_data_building(config, mock_well_known_config):
    """Test that request data is built correctly."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:
            with patch.object(client._http_client, "post", new_callable=AsyncMock) as mock_post:
                mock_response = MagicMock()
                mock_response.status_code = 200
                mock_response.json.return_value = {
                    "access_token": "new_token",
                    "issued_token_type": "urn:ietf:params:oauth:token-type:jwt",
                    "token_type": "Bearer",
                }
                mock_post.return_value = mock_response

                await client.delegate(
                    subject_token="subject_token",
                    subject_token_type=TokenType.JWT,
                    actor_token="actor_token",
                    actor_token_type=TokenType.JWT,
                    audience="https://api.example.com",
                    scope="read write",
                )

                # Verify the request was made with correct data
                mock_post.assert_called_once()
                call_args = mock_post.call_args

                # Check URL
                assert call_args[0][0] == "https://auth.example.com/oauth/token"

                # Check data
                data = call_args[1]["data"]
                assert data["grant_type"] == "urn:ietf:params:oauth:grant-type:token-exchange"
                assert data["subject_token"] == "subject_token"
                assert data["subject_token_type"] == "urn:ietf:params:oauth:token-type:jwt"
                assert data["actor_token"] == "actor_token"
                assert data["actor_token_type"] == "urn:ietf:params:oauth:token-type:jwt"
                assert data["audience"] == "https://api.example.com"
                assert data["scope"] == "read write"


@pytest.mark.asyncio
async def test_context_manager(config, mock_well_known_config):
    """Test async context manager functionality."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        async with STSClient(config) as client:
            assert client._well_known_config is not None
            assert client._http_client is not None

        # After context exit, client should be closed
        assert client._http_client is None


@pytest.mark.asyncio
async def test_manual_initialization_and_close(config, mock_well_known_config):
    """Test manual initialization and close."""
    with patch("agentsts.core.client._client.fetch_well_known_configuration") as mock_fetch:
        mock_fetch.return_value = mock_well_known_config

        client = STSClient(config)
        assert client._well_known_config is None
        assert client._http_client is None

        await client._initialize()
        assert client._well_known_config is not None
        assert client._http_client is not None

        await client.close()
        assert client._http_client is None
