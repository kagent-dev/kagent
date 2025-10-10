"""Extended tests for types.py to improve coverage."""

import pytest
from google.adk.agents import LoopAgent, SequentialAgent
from pydantic import ValidationError

from kagent.adk.types import SubAgentReference, WorkflowAgentConfig


class TestSubAgentReference:
    """Test SubAgentReference class."""

    def test_create_sub_agent_reference_minimal(self):
        """Test creating a SubAgentReference with minimal fields."""
        ref = SubAgentReference(name="test-agent")

        assert ref.name == "test-agent"
        assert ref.namespace == "default"
        assert ref.kind == "Agent"
        assert ref.description == ""

    def test_create_sub_agent_reference_full(self):
        """Test creating a SubAgentReference with all fields."""
        ref = SubAgentReference(
            name="test-agent",
            namespace="custom-ns",
            kind="CustomAgent",
            description="Test description",
        )

        assert ref.name == "test-agent"
        assert ref.namespace == "custom-ns"
        assert ref.kind == "CustomAgent"
        assert ref.description == "Test description"


class TestWorkflowAgentConfig:
    """Test WorkflowAgentConfig class."""

    def test_create_parallel_workflow_config(self):
        """Test creating a parallel workflow configuration."""
        sub_agents = [
            SubAgentReference(name="agent1"),
            SubAgentReference(name="agent2"),
        ]

        config = WorkflowAgentConfig(
            name="parallel-workflow",
            description="Test parallel workflow",
            workflow_type="parallel",
            sub_agents=sub_agents,
            max_workers=5,
        )

        assert config.name == "parallel-workflow"
        assert config.workflow_type == "parallel"
        assert config.max_workers == 5
        assert len(config.sub_agents) == 2

    def test_create_sequential_workflow_config(self):
        """Test creating a sequential workflow configuration."""
        sub_agents = [
            SubAgentReference(name="agent1"),
            SubAgentReference(name="agent2"),
        ]

        config = WorkflowAgentConfig(
            name="sequential-workflow",
            description="Test sequential workflow",
            workflow_type="sequential",
            sub_agents=sub_agents,
        )

        assert config.name == "sequential-workflow"
        assert config.workflow_type == "sequential"
        assert config.max_workers is None

    def test_create_loop_workflow_config(self):
        """Test creating a loop workflow configuration."""
        sub_agents = [SubAgentReference(name="agent1")]

        config = WorkflowAgentConfig(
            name="loop-workflow",
            description="Test loop workflow",
            workflow_type="loop",
            sub_agents=sub_agents,
            max_iterations=10,
        )

        assert config.name == "loop-workflow"
        assert config.workflow_type == "loop"
        assert config.max_iterations == 10

    def test_to_agent_parallel(self):
        """Test converting parallel workflow config to agent."""
        from kagent.adk.agents.parallel import KAgentParallelAgent

        sub_agents = [
            SubAgentReference(name="agent1", namespace="test-ns", description="Agent 1"),
            SubAgentReference(name="agent2", namespace="test-ns", description="Agent 2"),
        ]

        config = WorkflowAgentConfig(
            name="parallel-workflow",
            description="Test parallel workflow",
            namespace="test-ns",
            workflow_type="parallel",
            sub_agents=sub_agents,
            max_workers=3,
        )

        agent = config.to_agent()

        assert isinstance(agent, KAgentParallelAgent)
        assert agent.name == "parallel_workflow"  # Hyphens replaced with underscores
        assert agent.max_workers == 3
        assert len(agent.sub_agents) == 2

    def test_to_agent_sequential(self):
        """Test converting sequential workflow config to agent."""
        sub_agents = [
            SubAgentReference(name="agent1", namespace="test-ns"),
            SubAgentReference(name="agent2", namespace="test-ns"),
        ]

        config = WorkflowAgentConfig(
            name="sequential-workflow",
            description="Test sequential workflow",
            workflow_type="sequential",
            sub_agents=sub_agents,
        )

        agent = config.to_agent()

        assert isinstance(agent, SequentialAgent)
        assert agent.name == "sequential_workflow"
        assert len(agent.sub_agents) == 2

    def test_to_agent_loop(self):
        """Test converting loop workflow config to agent."""
        sub_agents = [SubAgentReference(name="agent1", namespace="test-ns")]

        config = WorkflowAgentConfig(
            name="loop-workflow",
            description="Test loop workflow",
            workflow_type="loop",
            sub_agents=sub_agents,
            max_iterations=5,
        )

        agent = config.to_agent()

        assert isinstance(agent, LoopAgent)
        assert agent.name == "loop_workflow"
        assert agent.max_iterations == 5

    def test_to_agent_loop_without_max_iterations_raises_error(self):
        """Test that converting loop workflow without max_iterations raises ValueError."""
        sub_agents = [SubAgentReference(name="agent1")]

        config = WorkflowAgentConfig(
            name="loop-workflow",
            description="Test loop workflow",
            workflow_type="loop",
            sub_agents=sub_agents,
        )

        with pytest.raises(ValueError, match="Loop agent requires max_iterations"):
            config.to_agent()

    def test_to_agent_empty_sub_agents_raises_error(self):
        """Test that converting workflow with no sub-agents raises ValueError."""
        config = WorkflowAgentConfig(
            name="empty-workflow",
            description="Test empty workflow",
            workflow_type="parallel",
            sub_agents=[],
        )

        with pytest.raises(ValueError, match="Workflow agent must have at least one sub-agent"):
            config.to_agent()

    def test_to_agent_invalid_workflow_type_raises_error(self):
        """Test that invalid workflow type raises ValueError."""
        sub_agents = [SubAgentReference(name="agent1")]

        # This should fail at Pydantic validation level
        with pytest.raises(ValidationError):
            WorkflowAgentConfig(
                name="invalid-workflow",
                description="Test invalid workflow",
                workflow_type="invalid",
                sub_agents=sub_agents,
            )

    def test_to_agent_with_hyphens_in_names(self):
        """Test that agent names with hyphens are converted to underscores."""
        sub_agents = [
            SubAgentReference(name="sub-agent-1", namespace="test-ns"),
        ]

        config = WorkflowAgentConfig(
            name="my-parallel-workflow",
            description="Test workflow",
            workflow_type="parallel",
            sub_agents=sub_agents,
            max_workers=2,
        )

        agent = config.to_agent()

        # Main agent name should have hyphens replaced
        assert agent.name == "my_parallel_workflow"
        # Sub-agent names should also have hyphens replaced
        assert agent.sub_agents[0].name == "sub_agent_1"

    def test_parallel_workflow_default_max_workers(self):
        """Test that parallel workflow uses default max_workers when not specified."""
        sub_agents = [SubAgentReference(name="agent1")]

        config = WorkflowAgentConfig(
            name="parallel-workflow",
            description="Test parallel workflow",
            workflow_type="parallel",
            sub_agents=sub_agents,
        )

        agent = config.to_agent()

        # Should use default max_workers of 10
        assert agent.max_workers == 10

    def test_workflow_with_custom_namespace(self):
        """Test workflow with custom namespace propagates to agent."""
        from kagent.adk.agents.parallel import KAgentParallelAgent

        sub_agents = [SubAgentReference(name="agent1", namespace="custom-ns")]

        config = WorkflowAgentConfig(
            name="parallel-workflow",
            description="Test parallel workflow",
            namespace="custom-ns",
            workflow_type="parallel",
            sub_agents=sub_agents,
            max_workers=2,
        )

        agent = config.to_agent()

        assert isinstance(agent, KAgentParallelAgent)
        assert agent.namespace == "custom-ns"

    def test_sub_agent_url_construction(self):
        """Test that sub-agent URLs are constructed correctly."""
        sub_agents = [
            SubAgentReference(name="my-agent", namespace="my-namespace"),
        ]

        config = WorkflowAgentConfig(
            name="test-workflow",
            description="Test",
            workflow_type="parallel",
            sub_agents=sub_agents,
        )

        agent = config.to_agent()

        # Verify the RemoteA2aAgent was created with correct URL pattern
        # URL should be http://my-agent.my-namespace:8080
        assert len(agent.sub_agents) == 1
