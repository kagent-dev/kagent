"""Unit tests for KAgentSequentialAgent with shared session propagation."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event
from google.adk.sessions import Session
from google.genai.types import Content, Part

from kagent.adk.agents.sequential import KAgentSequentialAgent


@pytest.fixture
def mock_session():
    """Create a mock session with a shared session ID."""
    session = MagicMock(spec=Session)
    session.id = "shared-session-123"
    session.user_id = "test-user"
    session.events = []
    return session


@pytest.fixture
def mock_parent_context(mock_session):
    """Create a mock parent InvocationContext."""
    context = MagicMock(spec=InvocationContext)
    context.session = mock_session
    context.user_id = "test-user"
    context.app_name = "test-workflow"
    return context


@pytest.fixture
def mock_sub_agents():
    """Create mock sub-agents for testing."""
    sub_agent_1 = MagicMock(spec=BaseAgent)
    sub_agent_1.name = "sub-agent-1"
    sub_agent_1.parent_agent = None  # Required by BaseAgent
    sub_agent_1.root_agent = None

    sub_agent_2 = MagicMock(spec=BaseAgent)
    sub_agent_2.name = "sub-agent-2"
    sub_agent_2.parent_agent = None  # Required by BaseAgent
    sub_agent_2.root_agent = None

    return [sub_agent_1, sub_agent_2]


class TestKAgentSequentialAgent:
    """Test suite for KAgentSequentialAgent."""

    def test_init(self):
        """Test KAgentSequentialAgent initialization."""
        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test sequential agent",
            sub_agents=[],
            namespace="default",
        )

        assert agent.name == "test_sequential"
        assert agent.description == "Test sequential agent"
        assert agent.namespace == "default"
        assert len(agent.sub_agents) == 0

    @pytest.mark.asyncio
    async def test_sequential_agent_passes_session_id(self, mock_parent_context, mock_sub_agents):
        """Test that KAgentSequentialAgent passes parent session ID to sub-agents.

        Verifies:
        - Sub-agents receive the SAME parent context (not cloned)
        - Session ID from parent is propagated to all sub-agents
        - All sub-agents execute with shared session ID

        This is T010 from tasks.md.
        """

        # Setup: Create event generators for mock sub-agents
        async def gen_events_1(_context):
            yield Event(
                author="sub-agent-1",
                content=Content(parts=[Part(text="Response from sub-agent-1")]),
            )

        async def gen_events_2(_context):
            yield Event(
                author="sub-agent-2",
                content=Content(parts=[Part(text="Response from sub-agent-2")]),
            )

        # Mock run_async to return the async generators
        mock_sub_agents[0].run_async = MagicMock(side_effect=gen_events_1)
        mock_sub_agents[1].run_async = MagicMock(side_effect=gen_events_2)

        # Create sequential agent
        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test agent",
            sub_agents=mock_sub_agents,
            namespace="default",
        )

        # Execute
        events = []
        async for event in agent.run_async(mock_parent_context):
            events.append(event)

        # Assert: Both sub-agents were called
        assert mock_sub_agents[0].run_async.called
        assert mock_sub_agents[1].run_async.called

        # CRITICAL: Verify sub-agents received the SAME parent context (not cloned)
        # This ensures session ID propagates correctly
        call_context_1 = mock_sub_agents[0].run_async.call_args[0][0]
        call_context_2 = mock_sub_agents[1].run_async.call_args[0][0]

        # Both should be the same object (identity check, not just equality)
        assert call_context_1 is mock_parent_context, "Sub-agent-1 should receive the SAME parent context, not a clone"
        assert call_context_2 is mock_parent_context, "Sub-agent-2 should receive the SAME parent context, not a clone"

        # Verify session IDs match
        assert call_context_1.session.id == "shared-session-123"
        assert call_context_2.session.id == "shared-session-123"

        # Verify events were yielded
        assert len(events) == 2
        assert events[0].author == "sub-agent-1"
        assert events[1].author == "sub-agent-2"

    @pytest.mark.asyncio
    async def test_sequential_agent_preserves_parent_context(self, mock_parent_context, mock_sub_agents):
        """Test that parent InvocationContext is preserved across sub-agents.

        Verifies:
        - Parent context attributes remain unchanged
        - Session reference is maintained
        - User ID and app name are preserved

        This is T011 from tasks.md.
        """
        # Setup
        original_session_id = mock_parent_context.session.id
        original_user_id = mock_parent_context.user_id
        original_app_name = mock_parent_context.app_name

        async def gen_events(_context):
            yield Event(
                author="sub-agent-1",
                content=Content(parts=[Part(text="Response")]),
            )

        mock_sub_agents[0].run_async = MagicMock(side_effect=gen_events)
        mock_sub_agents[1].run_async = MagicMock(side_effect=gen_events)

        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test agent",
            sub_agents=mock_sub_agents,
            namespace="default",
        )

        # Execute
        events = []
        async for event in agent.run_async(mock_parent_context):
            events.append(event)

        # Assert: Parent context unchanged
        assert mock_parent_context.session.id == original_session_id
        assert mock_parent_context.user_id == original_user_id
        assert mock_parent_context.app_name == original_app_name

        # Assert: Sub-agents received unmodified parent context
        for sub_agent in mock_sub_agents:
            call_context = sub_agent.run_async.call_args[0][0]
            assert call_context.session.id == original_session_id
            assert call_context.user_id == original_user_id

    @pytest.mark.asyncio
    async def test_sequential_agent_yields_all_events(self, mock_parent_context, mock_sub_agents):
        """Test that events from all sub-agents are yielded in order.

        Verifies:
        - Events from sub-agent-1 yielded first
        - Events from sub-agent-2 yielded second
        - Event order matches execution order
        - All events are yielded (none dropped)

        This is T012 from tasks.md.
        """

        # Setup: Sub-agents generate multiple events
        async def gen_events_1(_context):
            yield Event(
                author="sub-agent-1",
                content=Content(parts=[Part(text="Event 1 from sub-agent-1")]),
            )
            yield Event(
                author="sub-agent-1",
                content=Content(parts=[Part(text="Event 2 from sub-agent-1")]),
            )

        async def gen_events_2(_context):
            yield Event(
                author="sub-agent-2",
                content=Content(parts=[Part(text="Event 1 from sub-agent-2")]),
            )
            yield Event(
                author="sub-agent-2",
                content=Content(parts=[Part(text="Event 2 from sub-agent-2")]),
            )

        mock_sub_agents[0].run_async = MagicMock(side_effect=gen_events_1)
        mock_sub_agents[1].run_async = MagicMock(side_effect=gen_events_2)

        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test agent",
            sub_agents=mock_sub_agents,
            namespace="default",
        )

        # Execute
        events = []
        async for event in agent.run_async(mock_parent_context):
            events.append(event)

        # Assert: All events yielded in correct order
        assert len(events) == 4, "Should yield all 4 events (2 from each sub-agent)"

        # Check ordering: sub-agent-1 events first, then sub-agent-2 events
        assert events[0].author == "sub-agent-1"
        assert events[1].author == "sub-agent-1"
        assert events[2].author == "sub-agent-2"
        assert events[3].author == "sub-agent-2"

        # Check event content
        assert "Event 1 from sub-agent-1" in events[0].content.parts[0].text
        assert "Event 2 from sub-agent-1" in events[1].content.parts[0].text
        assert "Event 1 from sub-agent-2" in events[2].content.parts[0].text
        assert "Event 2 from sub-agent-2" in events[3].content.parts[0].text

    @pytest.mark.asyncio
    async def test_sequential_agent_error_handling(self, mock_parent_context, mock_sub_agents):
        """Test that errors in sub-agents are handled gracefully.

        Verifies:
        - Exceptions from sub-agents are propagated
        - Error events are yielded
        - Subsequent sub-agents may or may not execute (implementation-dependent)
        """

        # Setup: First sub-agent succeeds, second raises exception
        async def gen_events_success(_context):
            yield Event(
                author="sub-agent-1",
                content=Content(parts=[Part(text="Success")]),
            )

        async def gen_events_error(_context):
            raise ValueError("Sub-agent-2 failed")
            yield  # Make it a generator

        mock_sub_agents[0].run_async = MagicMock(side_effect=gen_events_success)
        mock_sub_agents[1].run_async = MagicMock(side_effect=gen_events_error)

        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test agent",
            sub_agents=mock_sub_agents,
            namespace="default",
        )

        # Execute: Should raise exception from sub-agent-2
        with pytest.raises(ValueError, match="Sub-agent-2 failed"):
            async for _event in agent.run_async(mock_parent_context):
                pass  # Collect events until error

    @pytest.mark.asyncio
    async def test_sequential_agent_empty_sub_agents(self, mock_parent_context):
        """Test sequential agent with no sub-agents."""
        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test agent",
            sub_agents=[],
            namespace="default",
        )

        # Execute: Should complete without yielding events
        events = []
        async for event in agent.run_async(mock_parent_context):
            events.append(event)

        assert len(events) == 0, "No events should be yielded with empty sub_agents"

    @pytest.mark.asyncio
    async def test_session_object_released_after_request(self, mock_sub_agents):
        """Test that session objects are garbage collected after workflow execution.

        This is T035 from tasks.md (User Story 3).

        Verifies:
        - Agent doesn't hold persistent references to session/context
        - Memory management follows per-request pattern
        - No lingering references after workflow execution
        """
        import gc
        import sys

        # Create a real Session object (not a mock) to test garbage collection
        from google.adk.sessions import Session

        real_session = Session(id="test-session-123", user_id="test-user", app_name="test-app", state={}, events=[])

        # Create context with real session
        real_context = MagicMock(spec=InvocationContext)
        real_context.session = real_session
        real_context.user_id = "test-user"
        real_context.app_name = "test-workflow"

        # Track initial reference count
        initial_session_refcount = sys.getrefcount(real_session)

        # Setup: Create event generators for sub-agents
        async def gen_events(_context):
            yield Event(
                author="sub-agent-1",
                content=Content(parts=[Part(text="Response")]),
            )

        mock_sub_agents[0].run_async = MagicMock(side_effect=gen_events)
        mock_sub_agents[1].run_async = MagicMock(side_effect=gen_events)

        # Create sequential agent
        agent = KAgentSequentialAgent(
            name="test_sequential",
            description="Test agent",
            sub_agents=mock_sub_agents,
            namespace="default",
        )

        # Execute workflow
        events = []
        async for event in agent.run_async(real_context):
            events.append(event)

        # Verify workflow completed successfully
        assert len(events) == 2, "Should have 2 events from 2 sub-agents"

        # KEY VERIFICATION: Agent doesn't hold persistent reference to context or session
        # This is the core memory management pattern
        assert not hasattr(agent, "_cached_context"), "Agent should not cache context"
        assert not hasattr(agent, "_cached_session"), "Agent should not cache session"
        assert not hasattr(agent, "_current_session"), "Agent should not store current session"

        # After workflow completes, reference count should return to initial
        # (accounting for temporary references during execution)
        final_session_refcount = sys.getrefcount(real_session)

        # The session should not have additional permanent references
        # Allow +/- 2 for temporary stack references
        assert abs(final_session_refcount - initial_session_refcount) <= 2, (
            f"Session reference count changed: initial={initial_session_refcount}, "
            f"final={final_session_refcount}. This may indicate a memory leak."
        )

        # Verify no references stored in agent's __dict__
        agent_attrs = vars(agent)
        for attr_name, attr_value in agent_attrs.items():
            assert attr_value is not real_session, f"Agent attribute '{attr_name}' holds reference to session"
            assert attr_value is not real_context, f"Agent attribute '{attr_name}' holds reference to context"
