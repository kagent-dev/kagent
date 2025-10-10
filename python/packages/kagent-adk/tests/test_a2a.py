"""Tests for kagent.adk._a2a module."""

from unittest.mock import AsyncMock, Mock, patch

import pytest
from a2a.types import AgentCard
from fastapi import Request
from fastapi.responses import PlainTextResponse
from google.adk.agents import BaseAgent
from google.adk.sessions import InMemorySessionService

from kagent.adk._a2a import KAgentApp, health_check, thread_dump


class TestHealthCheck:
    """Tests for health_check endpoint."""

    def test_health_check_returns_ok(self):
        """Test health check returns OK."""
        request = Mock(spec=Request)

        response = health_check(request)

        assert isinstance(response, PlainTextResponse)
        assert response.body.decode() == "OK"


class TestThreadDump:
    """Tests for thread_dump endpoint."""

    def test_thread_dump_returns_traceback(self):
        """Test thread dump returns traceback information."""
        request = Mock(spec=Request)

        with patch("faulthandler.dump_traceback") as mock_dump:
            mock_dump.return_value = None

            response = thread_dump(request)

            assert isinstance(response, PlainTextResponse)
            # Function was called to dump traceback
            mock_dump.assert_called_once()


class TestKAgentAppInit:
    """Tests for KAgentApp initialization."""

    def test_init(self):
        """Test KAgentApp initialization."""
        mock_agent = Mock(spec=BaseAgent)
        mock_card = Mock(spec=AgentCard)

        app = KAgentApp(
            root_agent=mock_agent, agent_card=mock_card, kagent_url="https://kagent.example.com", app_name="test-app"
        )

        assert app.root_agent == mock_agent
        assert app.agent_card == mock_card
        assert app.kagent_url == "https://kagent.example.com"
        assert app.app_name == "test-app"


class TestKAgentAppBuild:
    """Tests for KAgentApp.build() method."""

    @patch("kagent.adk._a2a.KAgentTokenService")
    @patch("kagent.adk._a2a.httpx.AsyncClient")
    @patch("kagent.adk._a2a.KAgentSessionService")
    @patch("kagent.adk._a2a.A2aAgentExecutor")
    @patch("kagent.adk._a2a.KAgentTaskStore")
    @patch("kagent.adk._a2a.KAgentRequestContextBuilder")
    @patch("kagent.adk._a2a.DefaultRequestHandler")
    @patch("kagent.adk._a2a.A2AFastAPIApplication")
    @patch("kagent.adk._a2a.FastAPI")
    def test_build_creates_fastapi_app(
        self,
        mock_fastapi,
        mock_a2a_app,
        mock_request_handler,
        mock_context_builder,
        mock_task_store,
        mock_executor,
        mock_session_service,
        mock_http_client,
        mock_token_service,
    ):
        """Test that build creates a FastAPI application with all components."""
        # Setup mocks
        mock_agent = Mock(spec=BaseAgent)
        mock_card = Mock(spec=AgentCard)

        mock_token_svc_instance = Mock()
        mock_token_svc_instance.event_hooks.return_value = {}
        mock_token_svc_instance.lifespan.return_value = AsyncMock()
        mock_token_service.return_value = mock_token_svc_instance

        mock_http_client_instance = Mock()
        mock_http_client.return_value = mock_http_client_instance

        mock_session_svc_instance = Mock()
        mock_session_service.return_value = mock_session_svc_instance

        mock_fastapi_instance = Mock()
        mock_fastapi.return_value = mock_fastapi_instance

        mock_a2a_instance = Mock()
        mock_a2a_app.return_value = mock_a2a_instance

        # Create app and build
        app = KAgentApp(
            root_agent=mock_agent, agent_card=mock_card, kagent_url="https://kagent.example.com", app_name="test-app"
        )

        result = app.build()

        # Verify components were created
        mock_token_service.assert_called_once_with("test-app")
        mock_http_client.assert_called_once()
        mock_session_service.assert_called_once_with(mock_http_client_instance)

        # Verify FastAPI app was created
        mock_fastapi.assert_called_once()
        assert result == mock_fastapi_instance

        # Verify routes were added
        assert mock_fastapi_instance.add_route.call_count == 2  # health and thread_dump
        mock_a2a_instance.add_routes_to_app.assert_called_once_with(mock_fastapi_instance)

    @patch("kagent.adk._a2a.kagent_url_override", "https://override.example.com")
    @patch("kagent.adk._a2a.KAgentTokenService")
    @patch("kagent.adk._a2a.httpx.AsyncClient")
    @patch("kagent.adk._a2a.KAgentSessionService")
    @patch("kagent.adk._a2a.A2aAgentExecutor")
    @patch("kagent.adk._a2a.KAgentTaskStore")
    @patch("kagent.adk._a2a.KAgentRequestContextBuilder")
    @patch("kagent.adk._a2a.DefaultRequestHandler")
    @patch("kagent.adk._a2a.A2AFastAPIApplication")
    @patch("kagent.adk._a2a.FastAPI")
    def test_build_uses_kagent_url_override(
        self,
        mock_fastapi,
        mock_a2a_app,
        mock_request_handler,
        mock_context_builder,
        mock_task_store,
        mock_executor,
        mock_session_service,
        mock_http_client,
        mock_token_service,
    ):
        """Test that kagent_url_override is used when set."""
        mock_agent = Mock(spec=BaseAgent)
        mock_card = Mock(spec=AgentCard)

        mock_token_svc_instance = Mock()
        mock_token_svc_instance.event_hooks.return_value = {}
        mock_token_svc_instance.lifespan.return_value = AsyncMock()
        mock_token_service.return_value = mock_token_svc_instance

        mock_http_client_instance = Mock()
        mock_http_client.return_value = mock_http_client_instance

        mock_fastapi_instance = Mock()
        mock_fastapi.return_value = mock_fastapi_instance

        mock_a2a_instance = Mock()
        mock_a2a_app.return_value = mock_a2a_instance

        app = KAgentApp(
            root_agent=mock_agent, agent_card=mock_card, kagent_url="https://original.example.com", app_name="test-app"
        )

        app.build()

        # Verify override URL was used
        call_kwargs = mock_http_client.call_args[1]
        assert call_kwargs["base_url"] == "https://override.example.com"


class TestKAgentAppTest:
    """Tests for KAgentApp.test() method."""

    @pytest.mark.asyncio
    async def test_test_method_with_agent_instance(self):
        """Test the test() method with an agent instance."""
        mock_agent = Mock(spec=BaseAgent)
        mock_card = Mock(spec=AgentCard)

        app = KAgentApp(
            root_agent=mock_agent, agent_card=mock_card, kagent_url="https://kagent.example.com", app_name="test-app"
        )

        with patch("kagent.adk._a2a.InMemorySessionService") as mock_session_service:
            mock_session_instance = AsyncMock(spec=InMemorySessionService)
            mock_session_service.return_value = mock_session_instance

            with patch("kagent.adk._a2a.Runner") as mock_runner:
                mock_runner_instance = Mock()

                # Create an async generator for run_async
                async def mock_run_async(*args, **kwargs):
                    # Create a mock event
                    mock_event = Mock()
                    mock_event.model_dump_json.return_value = '{"event": "test"}'
                    yield mock_event

                mock_runner_instance.run_async = mock_run_async
                mock_runner.return_value = mock_runner_instance

                await app.test("Test task")

                # Verify session was created
                mock_session_instance.create_session.assert_called_once()

                # Verify runner was created
                mock_runner.assert_called_once()

    @pytest.mark.asyncio
    async def test_test_method_with_agent_factory(self):
        """Test the test() method with an agent factory function."""
        mock_agent = Mock(spec=BaseAgent)
        mock_agent_factory = Mock(return_value=mock_agent)
        mock_card = Mock(spec=AgentCard)

        app = KAgentApp(
            root_agent=mock_agent_factory,  # Pass factory instead of instance
            agent_card=mock_card,
            kagent_url="https://kagent.example.com",
            app_name="test-app",
        )

        with patch("kagent.adk._a2a.InMemorySessionService") as mock_session_service:
            mock_session_instance = AsyncMock(spec=InMemorySessionService)
            mock_session_service.return_value = mock_session_instance

            with patch("kagent.adk._a2a.Runner") as mock_runner:
                mock_runner_instance = Mock()

                async def mock_run_async(*args, **kwargs):
                    mock_event = Mock()
                    mock_event.model_dump_json.return_value = '{"event": "test"}'
                    yield mock_event

                mock_runner_instance.run_async = mock_run_async
                mock_runner.return_value = mock_runner_instance

                await app.test("Test task with factory")

                # Verify factory was called
                mock_agent_factory.assert_called_once()

    @pytest.mark.asyncio
    async def test_test_method_creates_proper_content(self):
        """Test that test() method creates proper content for the agent."""
        mock_agent = Mock(spec=BaseAgent)
        mock_card = Mock(spec=AgentCard)

        app = KAgentApp(
            root_agent=mock_agent, agent_card=mock_card, kagent_url="https://kagent.example.com", app_name="test-app"
        )

        with patch("kagent.adk._a2a.InMemorySessionService") as mock_session_service:
            mock_session_instance = AsyncMock(spec=InMemorySessionService)
            mock_session_service.return_value = mock_session_instance

            with patch("kagent.adk._a2a.Runner") as mock_runner:
                mock_runner_instance = Mock()

                captured_kwargs = {}

                async def mock_run_async(*args, **kwargs):
                    captured_kwargs.update(kwargs)
                    mock_event = Mock()
                    mock_event.model_dump_json.return_value = "{}"
                    yield mock_event

                mock_runner_instance.run_async = mock_run_async
                mock_runner.return_value = mock_runner_instance

                test_task = "What is the weather?"
                await app.test(test_task)

                # Verify the content was created properly
                assert "new_message" in captured_kwargs
                assert captured_kwargs["new_message"].role == "user"
                assert len(captured_kwargs["new_message"].parts) == 1
                assert captured_kwargs["new_message"].parts[0].text == test_task


class TestIntegration:
    """Integration tests for KAgentApp."""

    def test_app_components_wired_correctly(self):
        """Test that all components are wired together correctly."""
        mock_agent = Mock(spec=BaseAgent)
        mock_card = Mock(spec=AgentCard)

        app = KAgentApp(
            root_agent=mock_agent,
            agent_card=mock_card,
            kagent_url="https://kagent.example.com",
            app_name="integration-test",
        )

        assert app.root_agent is not None
        assert app.agent_card is not None
        assert app.kagent_url is not None
        assert app.app_name is not None
