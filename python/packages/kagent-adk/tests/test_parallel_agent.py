"""Tests for the KAgentParallelAgent class."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from google.adk.agents import Agent
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events import Event
from google.adk.sessions import Session
from google.genai.types import Content, Part

from kagent.adk.agents.parallel import KAgentParallelAgent


class TestKAgentParallelAgentInit:
    """Test KAgentParallelAgent initialization."""

    def test_init_with_valid_max_workers(self):
        """Test initialization with valid max_workers value."""
        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=5,
            sub_agents=[],
        )

        assert agent.name == "test_parallel"
        assert agent.max_workers == 5
        assert agent.namespace == "default"
        assert agent.semaphore._value == 5

    def test_init_with_custom_namespace(self):
        """Test initialization with custom namespace."""
        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=3,
            namespace="custom-ns",
            sub_agents=[],
        )

        assert agent.namespace == "custom-ns"

    def test_init_with_invalid_max_workers_type(self):
        """Test that TypeError is raised for non-integer max_workers."""
        with pytest.raises(TypeError, match="max_workers must be int"):
            KAgentParallelAgent(
                name="test_parallel",
                description="Test",
                max_workers="5",  # String instead of int
                sub_agents=[],
            )

    def test_init_with_max_workers_too_small(self):
        """Test that ValueError is raised for max_workers < 1."""
        with pytest.raises(ValueError, match="max_workers must be >= 1"):
            KAgentParallelAgent(
                name="test_parallel",
                description="Test",
                max_workers=0,
                sub_agents=[],
            )

    def test_init_with_max_workers_too_large(self):
        """Test that ValueError is raised for max_workers > 50."""
        with pytest.raises(ValueError, match="max_workers must be <= 50"):
            KAgentParallelAgent(
                name="test_parallel",
                description="Test",
                max_workers=51,
                sub_agents=[],
            )

    def test_init_with_sub_agents(self):
        """Test initialization with sub-agents."""
        sub_agent1 = MagicMock(spec=BaseAgent)
        sub_agent1.name = "sub1"
        sub_agent1.parent_agent = None
        sub_agent2 = MagicMock(spec=BaseAgent)
        sub_agent2.name = "sub2"
        sub_agent2.parent_agent = None

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=2,
            sub_agents=[sub_agent1, sub_agent2],
        )

        assert len(agent.sub_agents) == 2


class TestRunAsync:
    """Test run_async method."""

    @pytest.mark.asyncio
    async def test_run_async_with_no_sub_agents(self):
        """Test run_async with no sub-agents."""
        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=5,
            sub_agents=[],
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)

        events = []
        async for event in agent.run_async(context):
            events.append(event)

        assert len(events) == 0

    @pytest.mark.asyncio
    async def test_run_async_with_successful_sub_agents(self):
        """Test run_async with successful sub-agents."""
        # Create mock sub-agents
        sub_agent1 = MagicMock(spec=BaseAgent)
        sub_agent1.name = "sub1"
        sub_agent1.parent_agent = None
        event1 = Event(
            author="sub1",
            content=Content(parts=[Part(text="Result from sub1")], role="model"),
        )

        async def sub1_run_async(ctx):
            yield event1

        sub_agent1.run_async = sub1_run_async

        sub_agent2 = MagicMock(spec=BaseAgent)
        sub_agent2.name = "sub2"
        sub_agent2.parent_agent = None
        event2 = Event(
            author="sub2",
            content=Content(parts=[Part(text="Result from sub2")], role="model"),
        )

        async def sub2_run_async(ctx):
            yield event2

        sub_agent2.run_async = sub2_run_async

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=2,
            sub_agents=[sub_agent1, sub_agent2],
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)
        # Create a shallow copy for model_copy to avoid deep copy issues
        context_copy = MagicMock(spec=InvocationContext)
        context_copy.session = context.session
        context.model_copy = MagicMock(return_value=context_copy)

        events = []
        async for event in agent.run_async(context):
            events.append(event)

        # Should get events from both sub-agents
        assert len(events) == 2
        event_authors = [e.author for e in events]
        assert "sub1" in event_authors
        assert "sub2" in event_authors

    @pytest.mark.asyncio
    async def test_run_async_with_failing_sub_agent(self):
        """Test run_async with one failing sub-agent."""
        # Create successful sub-agent
        sub_agent1 = MagicMock(spec=BaseAgent)
        sub_agent1.name = "sub1"
        sub_agent1.parent_agent = None
        event1 = Event(
            author="sub1",
            content=Content(parts=[Part(text="Result from sub1")], role="model"),
        )

        async def sub1_run_async(ctx):
            yield event1

        sub_agent1.run_async = sub1_run_async

        # Create failing sub-agent
        sub_agent2 = MagicMock(spec=BaseAgent)
        sub_agent2.name = "sub2"
        sub_agent2.parent_agent = None

        async def sub2_run_async(ctx):
            raise ValueError("Sub-agent 2 failed")

        sub_agent2.run_async = sub2_run_async

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=2,
            sub_agents=[sub_agent1, sub_agent2],
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)
        # Create a shallow copy for model_copy to avoid deep copy issues
        context_copy = MagicMock(spec=InvocationContext)
        context_copy.session = context.session
        context.model_copy = MagicMock(return_value=context_copy)

        events = []
        async for event in agent.run_async(context):
            events.append(event)

        # Should get success event from sub1 and error event from sub2
        assert len(events) == 2
        # Check for error event
        error_events = [e for e in events if hasattr(e, "error_code") and e.error_code == "SUB_AGENT_ERROR"]
        assert len(error_events) == 1
        assert "sub2" in str(error_events[0].content.parts[0].text)

    @pytest.mark.asyncio
    async def test_run_async_respects_max_workers(self):
        """Test that run_async respects max_workers limit."""
        # Create multiple sub-agents that track concurrent execution
        concurrent_count = {"current": 0, "max_seen": 0}
        lock = asyncio.Lock()

        async def create_tracking_sub_agent(name):
            agent = MagicMock(spec=BaseAgent)
            agent.name = name
            agent.parent_agent = None

            async def run_async(ctx):
                async with lock:
                    concurrent_count["current"] += 1
                    concurrent_count["max_seen"] = max(concurrent_count["max_seen"], concurrent_count["current"])

                # Simulate some work
                await asyncio.sleep(0.01)

                async with lock:
                    concurrent_count["current"] -= 1

                yield Event(
                    author=name,
                    content=Content(parts=[Part(text=f"Result from {name}")], role="model"),
                )

            agent.run_async = run_async
            return agent

        # Create 10 sub-agents but limit to 3 workers
        sub_agents = []
        for i in range(10):
            sub_agents.append(await create_tracking_sub_agent(f"sub{i}"))

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=3,
            sub_agents=sub_agents,
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)
        # Create a shallow copy for model_copy to avoid deep copy issues
        context_copy = MagicMock(spec=InvocationContext)
        context_copy.session = context.session
        context.model_copy = MagicMock(return_value=context_copy)

        events = []
        async for event in agent.run_async(context):
            events.append(event)

        # Check that we never exceeded max_workers
        assert concurrent_count["max_seen"] <= 3
        # All sub-agents should have completed
        assert len(events) == 10


class TestRunSubAgentWithSemaphore:
    """Test _run_sub_agent_with_semaphore method."""

    @pytest.mark.asyncio
    async def test_run_sub_agent_with_semaphore_success(self):
        """Test successful sub-agent execution with semaphore."""
        sub_agent = MagicMock(spec=BaseAgent)
        sub_agent.name = "test_sub"
        sub_agent.parent_agent = None
        event = Event(
            author="test_sub",
            content=Content(parts=[Part(text="Success")], role="model"),
        )

        async def run_async(ctx):
            yield event

        sub_agent.run_async = run_async

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=5,
            sub_agents=[sub_agent],
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)
        # Create a shallow copy for model_copy to avoid deep copy issues
        context_copy = MagicMock(spec=InvocationContext)
        context_copy.session = context.session
        context.model_copy = MagicMock(return_value=context_copy)

        events = await agent._run_sub_agent_with_semaphore(sub_agent, context, 0)

        assert len(events) == 1
        assert events[0] == event

    @pytest.mark.asyncio
    async def test_run_sub_agent_with_semaphore_failure(self):
        """Test sub-agent execution failure with semaphore."""
        sub_agent = MagicMock(spec=BaseAgent)
        sub_agent.name = "test_sub"
        sub_agent.parent_agent = None

        async def run_async(ctx):
            raise ValueError("Test error")
            yield  # Make it a generator

        sub_agent.run_async = run_async

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=5,
            sub_agents=[sub_agent],
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)
        # Create a shallow copy for model_copy to avoid deep copy issues
        context_copy = MagicMock(spec=InvocationContext)
        context_copy.session = context.session
        context.model_copy = MagicMock(return_value=context_copy)

        with pytest.raises(ValueError, match="Test error"):
            await agent._run_sub_agent_with_semaphore(sub_agent, context, 0)

    @pytest.mark.asyncio
    async def test_run_sub_agent_metrics_tracking(self):
        """Test that metrics are tracked during sub-agent execution."""
        sub_agent = MagicMock(spec=BaseAgent)
        sub_agent.name = "test_sub"
        sub_agent.parent_agent = None
        event = Event(
            author="test_sub",
            content=Content(parts=[Part(text="Success")], role="model"),
        )

        async def run_async(ctx):
            yield event

        sub_agent.run_async = run_async

        agent = KAgentParallelAgent(
            name="test_parallel",
            description="Test parallel agent",
            max_workers=5,
            namespace="test_ns",
            sub_agents=[sub_agent],
        )

        context = MagicMock(spec=InvocationContext)
        context.session = MagicMock(spec=Session)
        # Create a shallow copy for model_copy to avoid deep copy issues
        context_copy = MagicMock(spec=InvocationContext)
        context_copy.session = context.session
        context.model_copy = MagicMock(return_value=context_copy)

        # Execute and verify no exceptions
        events = await agent._run_sub_agent_with_semaphore(sub_agent, context, 0)
        assert len(events) == 1
