import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from a2a.types import AgentCard
from google.adk.agents import BaseAgent
from google.adk.auth import AuthCredential, AuthCredentialTypes
from google.adk.auth.auth_credential import HttpAuth, HttpCredentials
from google.adk.tools.mcp_tool import MCPToolset, StreamableHTTPConnectionParams

from kagent.adk._a2a import KAgentApp
from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._service_account_service import KAgentServiceAccountService
from kagent.adk._sts_token_service import KAgentSTSTokenService


class MockMCPToolset:
    """Mock MCPToolset for testing auth_credential injection."""

    def __init__(self, connection_params=None):
        self.connection_params = connection_params
        self.auth_credential = None
        self.tool_name = "test-mcp-tool"


class MockAgent(BaseAgent):
    """Mock agent for testing."""

    def __init__(self, tools=None):
        super().__init__(name="test_agent")
        object.__setattr__(self, "tools", tools or [])


class MockRunner:
    """Mock runner for testing."""

    def __init__(self, agent, app_name, session_service):
        self.agent = agent
        self.app_name = app_name
        self.session_service = session_service


class TestAuthenticationFlow:
    """Integration tests for the complete authentication flow."""

    @pytest.fixture
    def mock_sts_config(self):
        """Mock STS configuration."""
        return {
            "well_known_uri": "https://sts.example.com/.well-known/oauth-authorization-server",
            "client_id": "test-client-id",
            "client_secret": "test-client-secret",
            "refresh_interval": 300,
        }

    @pytest.fixture
    def mock_service_account_service(self):
        """Mock service account service."""
        service = MagicMock(spec=KAgentServiceAccountService)
        service.get_actor_jwt = AsyncMock(return_value="mock-service-account-jwt")
        service.get_service_account_info = AsyncMock(
            return_value={
                "name": "test-app",
                "namespace": "test-namespace",
                "token": "mock-service-account-jwt",
                "identifier": "system:serviceaccount:test-namespace:test-app",
            }
        )
        return service

    @pytest.fixture
    def mock_mcp_tools(self):
        """Create mock MCP tools."""
        tool1 = MockMCPToolset(connection_params=StreamableHTTPConnectionParams(url="https://api1.example.com"))
        tool2 = MockMCPToolset(connection_params=StreamableHTTPConnectionParams(url="https://api2.example.com"))
        return [tool1, tool2]

    @pytest.fixture
    def mock_agent(self, mock_mcp_tools):
        """Create mock agent with MCP tools."""
        return MockAgent(tools=mock_mcp_tools)

    @pytest.fixture
    def mock_runner(self, mock_agent):
        """Create mock runner."""
        return MockRunner(agent=mock_agent, app_name="test-app", session_service=AsyncMock())

    @pytest.fixture
    def agent_executor(self, mock_service_account_service, mock_sts_config):
        """Create agent executor with mocked dependencies."""

        def create_runner():
            return MockRunner(agent=MockAgent(tools=[]), app_name="test-app", session_service=AsyncMock())

        # Create a mock STS service
        mock_sts_service = AsyncMock()
        mock_sts_service.exchange_token = AsyncMock(return_value="test-token")

        with patch("kagent.adk._agent_executor.asyncio.create_task") as mock_create_task:
            mock_create_task.return_value = AsyncMock()
            return A2aAgentExecutor(
                runner=create_runner, service_account_service=mock_service_account_service, sts_service=mock_sts_service
            )

    @pytest.mark.asyncio
    async def test_setup_request_authentication_success(self, agent_executor, mock_runner, mock_sts_config):
        """Test successful request authentication setup with STS delegation."""
        # Mock request context with JWT
        mock_context = MagicMock()
        mock_context.call_context.state.get.return_value = {"Authorization": "Bearer user-jwt-token"}

        # Mock the STS service's exchange_token method
        mock_sts_instance = AsyncMock()
        mock_sts_instance.exchange_token = AsyncMock(return_value="delegated-access-token")
        agent_executor._sts_service = mock_sts_instance

        # Test authentication setup
        await agent_executor._setup_request_authentication(mock_context, mock_runner)

        # Verify STS token service was called with correct parameters
        mock_sts_instance.exchange_token.assert_called_once()
        call_args = mock_sts_instance.exchange_token.call_args
        assert call_args.kwargs["subject_token"] == "user-jwt-token"
        assert call_args.kwargs["actor_token"] is not None  # Service account token

    @pytest.mark.asyncio
    async def test_store_token_in_session(self, agent_executor):
        """Test storing access token in session service."""
        access_token = "test-access-token-123"

        # Create a mock runner that will be returned by _resolve_runner
        mock_runner = MagicMock()
        mock_runner.session_service.set_access_token = MagicMock()

        # Mock the _resolve_runner method to return our mock runner
        with patch.object(agent_executor, "_resolve_runner", return_value=mock_runner):
            # Test token storage
            agent_executor._store_token_in_session(access_token, mock_runner)

            # Verify token was stored in session service
            mock_runner.session_service.set_access_token.assert_called_once_with(access_token)

    @pytest.mark.asyncio
    async def test_store_token_in_session_no_session_service(self, agent_executor):
        """Test storing token when session service doesn't have set_access_token method."""
        access_token = "test-access-token-456"

        # Mock runner without set_access_token method
        mock_runner = MagicMock()
        mock_runner.session_service = MagicMock()  # No set_access_token method

        # Mock the _resolve_runner method to return our mock runner
        with patch.object(agent_executor, "_resolve_runner", return_value=mock_runner):
            # This should not raise an error, just log a warning
            agent_executor._store_token_in_session(access_token, mock_runner)

    @pytest.mark.asyncio
    async def test_extract_jwt_from_context_success(self, agent_executor):
        """Test successful JWT extraction from request context."""
        mock_context = MagicMock()
        mock_context.call_context.state.get.side_effect = lambda key, default=None: {
            "headers": {"Authorization": "Bearer test-jwt-token"}
        }.get(key, default)

        result = agent_executor._extract_jwt_from_context(mock_context)
        assert result == "test-jwt-token"

    @pytest.mark.asyncio
    async def test_extract_jwt_from_context_invalid_header(self, agent_executor):
        """Test JWT extraction with invalid Authorization header."""
        mock_context = MagicMock()
        mock_context.call_context.state.get.side_effect = lambda key, default=None: {
            "headers": {"Authorization": "Invalid test-jwt-token"}
        }.get(key, default)

        with pytest.raises(ValueError, match="Authorization header must start with Bearer"):
            agent_executor._extract_jwt_from_context(mock_context)

    @pytest.mark.asyncio
    async def test_extract_jwt_from_context(self, agent_executor):
        """Test JWT extraction from request context."""
        # Test with valid Authorization header
        mock_context = MagicMock()
        mock_context.call_context.state.get.side_effect = lambda key, default=None: {
            "headers": {"Authorization": "Bearer test-jwt-token"}
        }.get(key, default)

        result = agent_executor._extract_jwt_from_context(mock_context)
        assert result == "test-jwt-token"

        # Test with invalid Authorization header
        mock_context.call_context.state.get.side_effect = lambda key, default=None: {
            "headers": {"Authorization": "Invalid test-jwt-token"}
        }.get(key, default)

        with pytest.raises(ValueError, match="Authorization header must start with Bearer"):
            agent_executor._extract_jwt_from_context(mock_context)

        # Test with no Authorization header
        mock_context.call_context.state.get.side_effect = lambda key, default=None: {"headers": {}}.get(key, default)

        with pytest.raises(ValueError, match="No Authorization header found in request"):
            agent_executor._extract_jwt_from_context(mock_context)

        # Test with no call context
        mock_context.call_context = None

        with pytest.raises(ValueError, match="No call context available"):
            agent_executor._extract_jwt_from_context(mock_context)

    @pytest.mark.asyncio
    async def test_full_authentication_flow(self, mock_service_account_service, mock_sts_config, mock_mcp_tools):
        """Test the complete authentication flow from request to token storage."""
        # Create agent with MCP tools
        agent = MockAgent(tools=mock_mcp_tools)

        # Create a mock session service
        mock_session_service = AsyncMock()
        mock_session_service.set_access_token = MagicMock()

        def create_runner():
            return MockRunner(agent=agent, app_name="test-app", session_service=mock_session_service)

        # Create a mock STS service
        mock_sts_service = AsyncMock()
        mock_sts_service.exchange_token = AsyncMock(return_value="delegated-access-token-789")

        # Create agent executor
        executor = A2aAgentExecutor(
            runner=create_runner, service_account_service=mock_service_account_service, sts_service=mock_sts_service
        )

        # Mock request context
        mock_context = MagicMock()
        mock_context.call_context.state.get.return_value = {"Authorization": "Bearer user-jwt-token"}

        # Mock the _resolve_runner method to return our mock runner
        with patch.object(executor, "_resolve_runner", return_value=create_runner()):
            # Execute the full authentication flow
            await executor._setup_request_authentication(mock_context, create_runner())

        # Verify STS service was called
        mock_sts_service.exchange_token.assert_called_once()
        call_args = mock_sts_service.exchange_token.call_args
        assert call_args.kwargs["subject_token"] == "user-jwt-token"
        assert call_args.kwargs["actor_token"] is not None  # Service account token

        # Verify token was stored in session service
        mock_session_service.set_access_token.assert_called_once_with("delegated-access-token-789")

    @pytest.mark.asyncio
    async def test_kagent_app_integration(self, mock_sts_config):
        """Test KAgentApp integration with authentication flow."""
        # Mock agent card
        agent_card = AgentCard(
            name="test-agent",
            description="Test agent for authentication",
            version="1.0.0",
            capabilities={},
            defaultInputModes=[],
            defaultOutputModes=[],
            skills=[],
            url="https://test-agent.example.com",
        )

        # Create mock agent
        mock_agent = MockAgent(tools=[])

        # Create KAgentApp
        app = KAgentApp(
            root_agent=mock_agent, agent_card=agent_card, kagent_url="https://kagent.example.com", app_name="test-app"
        )

        # Set STS config
        app.sts_config = mock_sts_config

        # Mock the build process
        with (
            patch("kagent.adk._a2a.KAgentTokenService") as mock_token_service,
            patch("kagent.adk._a2a.KAgentServiceAccountService") as mock_sa_service,
            patch("kagent.adk._a2a.httpx.AsyncClient") as mock_http_client,
            patch("kagent.adk._a2a.KAgentSessionService") as mock_session_service,
            patch("kagent.adk._a2a.A2aAgentExecutor") as mock_executor,
            patch("kagent.adk._a2a.KAgentTaskStore") as mock_task_store,
            patch("kagent.adk._a2a.KAgentRequestContextBuilder") as mock_context_builder,
            patch("kagent.adk._a2a.DefaultRequestHandler") as mock_request_handler,
            patch("kagent.adk._a2a.A2AFastAPIApplication") as mock_a2a_app,
            patch("kagent.adk._a2a.FastAPI") as mock_fastapi,
        ):
            # Mock service instances
            mock_token_instance = MagicMock()
            mock_sa_instance = MagicMock()
            mock_http_instance = MagicMock()
            mock_session_instance = MagicMock()
            mock_executor_instance = MagicMock()
            mock_task_store_instance = MagicMock()
            mock_context_builder_instance = MagicMock()
            mock_request_handler_instance = MagicMock()
            mock_a2a_app_instance = MagicMock()
            mock_fastapi_instance = MagicMock()

            mock_token_service.return_value = mock_token_instance
            mock_sa_service.return_value = mock_sa_instance
            mock_http_client.return_value = mock_http_instance
            mock_session_service.return_value = mock_session_instance
            mock_executor.return_value = mock_executor_instance
            mock_task_store.return_value = mock_task_store_instance
            mock_context_builder.return_value = mock_context_builder_instance
            mock_request_handler.return_value = mock_request_handler_instance
            mock_a2a_app.return_value = mock_a2a_app_instance
            mock_fastapi.return_value = mock_fastapi_instance

            # Build the app
            result = app.build()

            # Verify services were created
            mock_token_service.assert_called_once_with("test-app")
            mock_sa_service.assert_called_once_with("test-app")

            # Verify agent executor was created with correct parameters
            mock_executor.assert_called_once()
            call_args = mock_executor.call_args[1]
            assert call_args["service_account_service"] == mock_sa_instance

            # Verify FastAPI app was created
            mock_fastapi.assert_called_once()
            assert result == mock_fastapi_instance
