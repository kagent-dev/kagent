"""Integration tests for OpenAI Agents SDK integration with KAgent."""

import pytest
from unittest.mock import AsyncMock, Mock, patch
from agents.agent import Agent
from a2a.types import AgentCard, Message, Part, Role, TextPart

from kagent.openai.agent import KAgentApp
from kagent.openai.agent._session_service import KAgentSession, KAgentSessionFactory
from kagent.openai.agent._agent_executor import OpenAIAgentExecutor


@pytest.fixture
def simple_agent():
    """Create a simple test agent."""
    return Agent(
        name="TestAgent",
        instructions="You are a helpful test assistant.",
    )


@pytest.fixture
def agent_card():
    """Create a test agent card."""
    return AgentCard(
        name="test-openai-agent",
        description="Test OpenAI agent",
        version="0.1.0",
        capabilities={"streaming": True},
        defaultInputModes=["text"],
        defaultOutputModes=["text"],
    )


class TestKAgentSession:
    """Tests for KAgentSession."""

    @pytest.mark.asyncio
    async def test_session_creation(self):
        """Test that sessions can be created."""
        mock_client = AsyncMock()
        mock_client.get.return_value.status_code = 404
        mock_client.post.return_value.status_code = 200
        mock_client.post.return_value.json.return_value = {
            "data": {
                "id": "test-session",
                "user_id": "test-user",
            }
        }

        session = KAgentSession(
            session_id="test-session",
            client=mock_client,
            app_name="test-app",
            user_id="test-user",
        )

        await session._ensure_session_exists()

        # Verify session was created
        assert mock_client.post.called

    @pytest.mark.asyncio
    async def test_add_items(self):
        """Test adding items to a session."""
        mock_client = AsyncMock()
        mock_client.get.return_value.status_code = 200
        mock_client.get.return_value.json.return_value = {"data": {"session": {"id": "test-session"}, "events": []}}
        mock_client.post.return_value.status_code = 200

        session = KAgentSession(
            session_id="test-session",
            client=mock_client,
            app_name="test-app",
            user_id="test-user",
        )

        items = [
            {"role": "user", "content": "Hello"},
            {"role": "assistant", "content": "Hi there!"},
        ]

        await session.add_items(items)

        # Verify items were stored
        assert mock_client.post.call_count >= 1

    @pytest.mark.asyncio
    async def test_get_items_empty_session(self):
        """Test getting items from an empty session."""
        mock_client = AsyncMock()
        mock_client.get.return_value.status_code = 404

        session = KAgentSession(
            session_id="test-session",
            client=mock_client,
            app_name="test-app",
            user_id="test-user",
        )

        items = await session.get_items()

        assert items == []

    @pytest.mark.asyncio
    async def test_clear_session(self):
        """Test clearing a session."""
        mock_client = AsyncMock()
        mock_client.delete.return_value.status_code = 200

        session = KAgentSession(
            session_id="test-session",
            client=mock_client,
            app_name="test-app",
            user_id="test-user",
        )

        await session.clear_session()

        # Verify delete was called
        assert mock_client.delete.called


class TestKAgentSessionFactory:
    """Tests for KAgentSessionFactory."""

    def test_create_session(self):
        """Test that factory creates sessions correctly."""
        mock_client = AsyncMock()
        factory = KAgentSessionFactory(
            client=mock_client,
            app_name="test-app",
            default_user_id="test-user",
        )

        session = factory.create_session("test-session")

        assert session.session_id == "test-session"
        assert session.app_name == "test-app"
        assert session.user_id == "test-user"

    def test_create_session_with_custom_user(self):
        """Test factory with custom user ID."""
        mock_client = AsyncMock()
        factory = KAgentSessionFactory(
            client=mock_client,
            app_name="test-app",
            default_user_id="default-user",
        )

        session = factory.create_session("test-session", user_id="custom-user")

        assert session.user_id == "custom-user"


class TestOpenAIAgentExecutor:
    """Tests for OpenAIAgentExecutor."""

    def test_executor_initialization(self, simple_agent):
        """Test executor can be initialized."""
        executor = OpenAIAgentExecutor(
            agent=simple_agent,
            app_name="test-app",
        )

        assert executor.app_name == "test-app"
        assert executor._agent == simple_agent

    def test_executor_with_factory(self):
        """Test executor with agent factory function."""

        def agent_factory():
            return Agent(name="FactoryAgent", instructions="Test")

        executor = OpenAIAgentExecutor(
            agent=agent_factory,
            app_name="test-app",
        )

        agent = executor._resolve_agent()
        assert agent.name == "FactoryAgent"


class TestKAgentApp:
    """Tests for KAgentApp."""

    def test_app_initialization(self, simple_agent, agent_card):
        """Test KAgentApp can be initialized."""
        app = KAgentApp(
            agent=simple_agent,
            agent_card=agent_card,
            kagent_url="http://localhost:8080",
            app_name="test-app",
        )

        assert app.app_name == "test-app"
        assert app.kagent_url == "http://localhost:8080"

    def test_build_local(self, simple_agent, agent_card):
        """Test building a local FastAPI application."""
        app = KAgentApp(
            agent=simple_agent,
            agent_card=agent_card,
            kagent_url="http://localhost:8080",
            app_name="test-app",
        )

        fastapi_app = app.build_local()

        # Verify FastAPI app was created
        assert fastapi_app is not None
        # Check that health endpoints exist
        routes = [route.path for route in fastapi_app.routes]
        assert "/health" in routes
        assert "/thread_dump" in routes

    @pytest.mark.asyncio
    async def test_agent_test_method(self, simple_agent, agent_card):
        """Test the test() method."""
        app = KAgentApp(
            agent=simple_agent,
            agent_card=agent_card,
            kagent_url="http://localhost:8080",
            app_name="test-app",
        )

        # Mock the Runner.run method
        with patch("kagent.openai._a2a.Runner.run") as mock_run:
            mock_result = Mock()
            mock_result.final_output = "Test response"
            mock_run.return_value = mock_result

            await app.test("Test question")

            # Verify runner was called
            assert mock_run.called


class TestEventConverter:
    """Tests for event conversion."""

    def test_run_item_event_conversion(self):
        """Test converting run item stream events."""
        from kagent.openai._event_converter import convert_openai_event_to_a2a_events
        from agents.stream_events import RunItemStreamEvent
        from agents.items import MessageOutputItem
        from unittest.mock import Mock

        # Create a mock message output item
        mock_item = Mock(spec=MessageOutputItem)
        mock_content = Mock()
        mock_content.text = "Hello from agent"
        mock_item.content = [mock_content]

        # Create the event
        event = RunItemStreamEvent(
            name="message_output_created",
            item=mock_item,
        )

        a2a_events = convert_openai_event_to_a2a_events(
            event,
            task_id="test-task",
            context_id="test-context",
            app_name="test-app",
        )

        assert len(a2a_events) > 0
        # Verify it's a TaskStatusUpdateEvent
        assert hasattr(a2a_events[0], "task_id")
        assert a2a_events[0].task_id == "test-task"
