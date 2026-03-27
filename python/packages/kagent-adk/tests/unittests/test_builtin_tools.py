import sys
from unittest.mock import MagicMock

import pytest


@pytest.fixture(autouse=True)
def _isolate_types_import(monkeypatch):
    stubs = [
        "kagent.core",
        "kagent.core.a2a",
        "kagent.core.tracing",
        "kagent.core.tracing._span_processor",
        "agentsts",
        "agentsts.adk",
    ]
    for mod_name in stubs:
        if mod_name not in sys.modules:
            monkeypatch.setitem(sys.modules, mod_name, MagicMock())


def _make_agent_config(builtin_tools=None):
    import kagent.adk.types as types_mod

    kwargs = {
        "model": {"type": "gemini", "model": "gemini-2.0-flash"},
        "description": "test agent",
        "instruction": "do stuff",
    }
    if builtin_tools is not None:
        kwargs["builtin_tools"] = builtin_tools
    return types_mod.AgentConfig(**kwargs)


def _has_ask_user_tool(agent) -> bool:
    from kagent.adk.tools.ask_user_tool import AskUserTool

    return any(isinstance(t, AskUserTool) for t in agent.tools)


class TestBuiltinTools:
    def test_ask_user_added_when_enabled(self):
        config = _make_agent_config(builtin_tools=["ask_user"])
        agent = config.to_agent("test_agent")
        assert _has_ask_user_tool(agent)

    def test_ask_user_not_added_when_none(self):
        config = _make_agent_config(builtin_tools=None)
        agent = config.to_agent("test_agent")
        assert not _has_ask_user_tool(agent)

    def test_ask_user_not_added_when_empty_list(self):
        config = _make_agent_config(builtin_tools=[])
        agent = config.to_agent("test_agent")
        assert not _has_ask_user_tool(agent)

    def test_unknown_builtin_tool_silently_skipped(self):
        config = _make_agent_config(builtin_tools=["unknown_tool"])
        agent = config.to_agent("test_agent")
        assert not _has_ask_user_tool(agent)

    def test_ask_user_with_other_unknown_tools(self):
        config = _make_agent_config(builtin_tools=["unknown", "ask_user", "another"])
        agent = config.to_agent("test_agent")
        assert _has_ask_user_tool(agent)

    def test_default_omits_builtin_tools(self):
        config = _make_agent_config()
        assert config.builtin_tools is None
