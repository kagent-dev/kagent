"""The optional ask_user setting preserves defaults and survives JSON parsing."""

import pytest

from kagent.adk.tools.ask_user_tool import AskUserTool
from kagent.adk.types import AgentConfig, create_agent, parse_config


@pytest.mark.parametrize("enabled", [None, True, False])
def test_ask_user_availability(enabled):
    agent_config = {"model": {"model": "gemini-3.5-flash", "gemini": {}}}
    if enabled is not None:
        agent_config["ask_user"] = enabled
    config = parse_config({"agent": agent_config})
    assert config.agent.HasField("ask_user") is (enabled is not None)

    agent = create_agent(config.agent, "assistant")
    question_tools = [tool for tool in agent.tools if isinstance(tool, AskUserTool)]
    assert len(question_tools) == (0 if enabled is False else 1)


def test_disabling_questions_keeps_tool_approval_callback():
    config = AgentConfig(
        model={"model": "gemini-3.5-flash", "gemini": {}},
        ask_user=False,
        http_tools=[{"params": {"url": "http://mcp.example.com/mcp"}, "require_approval": True}],
    )
    agent = create_agent(config, "assistant")
    assert agent.before_tool_callback is not None
    assert not any(isinstance(tool, AskUserTool) for tool in agent.tools)
    assert agent.tools  # The approval-gated MCP toolset remains configured.
