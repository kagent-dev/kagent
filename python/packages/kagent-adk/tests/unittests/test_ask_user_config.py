"""The runtime-wide question-tool setting preserves defaults and approvals."""

import pytest

from kagent.adk.tools.ask_user_tool import AskUserTool
from kagent.adk.types import create_agent, parse_config


@pytest.mark.parametrize("disabled", [None, False, True])
def test_ask_user_availability(disabled):
    data = {"agent": {"model": {"model": "gemini-3.5-flash", "gemini": {}}}}
    if disabled is not None:
        data["runtime"] = {"builtin_tools": {"disable_ask_user": disabled}}
    config = parse_config(data)
    agent = create_agent(config.agent, "assistant", runtime_config=config.runtime)
    question_tools = [tool for tool in agent.tools if isinstance(tool, AskUserTool)]
    assert len(question_tools) == (0 if disabled else 1)


def test_create_agent_without_runtime_keeps_questions_enabled():
    config = parse_config({"agent": {"model": {"model": "gemini-3.5-flash", "gemini": {}}}})
    agent = create_agent(config.agent, "assistant")
    assert any(isinstance(tool, AskUserTool) for tool in agent.tools)


def test_disabling_questions_keeps_tool_approval_callback():
    config = parse_config(
        {
            "runtime": {"builtin_tools": {"disable_ask_user": True}},
            "agent": {
                "model": {"model": "gemini-3.5-flash", "gemini": {}},
                "http_tools": [{"params": {"url": "http://mcp.example.com/mcp"}, "require_approval": True}],
            },
        }
    )
    agent = create_agent(config.agent, "assistant", runtime_config=config.runtime)
    assert agent.before_tool_callback is not None
    assert not any(isinstance(tool, AskUserTool) for tool in agent.tools)
    assert agent.tools  # The approval-gated MCP toolset remains configured.
