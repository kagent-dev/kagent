"""Tests for workflow agent construction from AgentConfig."""

from unittest.mock import MagicMock, patch

import pytest
from google.adk.agents import LoopAgent, ParallelAgent, SequentialAgent
from google.adk.models import BaseLlm

from kagent.adk.types import AgentConfig, SubAgentConfig, WorkflowAgentConfig


def _make_model_dict() -> dict:
    """Create a minimal OpenAI model config dict."""
    return {
        "type": "openai",
        "model": "gpt-4o",
        "base_url": "",
    }


def _make_mock_llm():
    """Create a mock LLM that passes pydantic validation."""
    mock = MagicMock(spec=BaseLlm)
    mock.model = "gpt-4o"
    return mock


def _make_agent_config(workflow_type: str, max_iterations: int | None = None) -> AgentConfig:
    """Create an AgentConfig with a workflow configuration."""
    sub_agents = [
        SubAgentConfig(
            name="writer",
            description="Writes content",
            instruction="You are a writer.",
            model=_make_model_dict(),
        ),
        SubAgentConfig(
            name="critic",
            description="Reviews content",
            instruction="You are a critic.",
            model=_make_model_dict(),
        ),
    ]

    workflow = WorkflowAgentConfig(
        type=workflow_type,
        sub_agents=sub_agents,
        max_iterations=max_iterations,
    )

    return AgentConfig(
        model=_make_model_dict(),
        description="Test workflow agent",
        instruction="",
        workflow=workflow,
    )


@patch("kagent.adk.types._create_llm_from_model_config")
def test_sequential_workflow(mock_llm):
    """to_agent() returns a SequentialAgent for type='sequential'."""
    mock_llm.return_value = _make_mock_llm()
    config = _make_agent_config("sequential")
    agent = config.to_agent("test_sequential")
    assert isinstance(agent, SequentialAgent)
    assert agent.name == "test_sequential"
    assert len(agent.sub_agents) == 2
    assert agent.sub_agents[0].name == "writer"
    assert agent.sub_agents[1].name == "critic"


@patch("kagent.adk.types._create_llm_from_model_config")
def test_parallel_workflow(mock_llm):
    """to_agent() returns a ParallelAgent for type='parallel'."""
    mock_llm.return_value = _make_mock_llm()
    config = _make_agent_config("parallel")
    agent = config.to_agent("test_parallel")
    assert isinstance(agent, ParallelAgent)
    assert agent.name == "test_parallel"
    assert len(agent.sub_agents) == 2


@patch("kagent.adk.types._create_llm_from_model_config")
def test_loop_workflow(mock_llm):
    """to_agent() returns a LoopAgent for type='loop' with max_iterations."""
    mock_llm.return_value = _make_mock_llm()
    config = _make_agent_config("loop", max_iterations=5)
    agent = config.to_agent("test_loop")
    assert isinstance(agent, LoopAgent)
    assert agent.name == "test_loop"
    assert len(agent.sub_agents) == 2
    assert agent.max_iterations == 5


@patch("kagent.adk.types._create_llm_from_model_config")
def test_loop_workflow_no_max_iterations(mock_llm):
    """to_agent() returns a LoopAgent with no max_iterations when not set."""
    mock_llm.return_value = _make_mock_llm()
    config = _make_agent_config("loop")
    agent = config.to_agent("test_loop_no_max")
    assert isinstance(agent, LoopAgent)
    assert agent.max_iterations is None


@patch("kagent.adk.types._create_llm_from_model_config")
def test_unknown_workflow_type(mock_llm):
    """to_agent() raises ValueError for unknown workflow type."""
    mock_llm.return_value = _make_mock_llm()
    # Use model_construct to bypass pydantic validation for Literal type
    workflow = WorkflowAgentConfig.model_construct(
        type="unknown",
        sub_agents=[
            SubAgentConfig(
                name="agent1",
                instruction="test",
                model=_make_model_dict(),
            ),
        ],
    )
    config = AgentConfig(
        model=_make_model_dict(),
        description="Test",
        instruction="",
        workflow=workflow,
    )
    with pytest.raises(ValueError, match="Unknown workflow type"):
        config.to_agent("test_unknown")


def test_no_workflow_returns_llm_agent():
    """AgentConfig without workflow has workflow=None."""
    config = AgentConfig(
        model=_make_model_dict(),
        description="Test regular agent",
        instruction="You are helpful.",
    )
    assert config.workflow is None
