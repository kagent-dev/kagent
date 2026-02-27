"""Tests for curly brace escaping in agent instructions.

Verifies that agent prompts containing curly braces (like {repo}) don't cause
KeyError from ADK's session state injection. See:
https://github.com/kagent-dev/kagent/issues/1382
"""

import importlib
import sys
from unittest.mock import MagicMock

import pytest


@pytest.fixture(autouse=True)
def _isolate_types_import(monkeypatch):
    """Ensure kagent.adk.types can be imported without the heavy dependency chain.

    kagent.adk.__init__ pulls in kagent.core (tracing, opentelemetry, etc.)
    which may not be installed in the test environment. We mock the missing
    modules so the types module itself can be imported directly.
    """
    # These are only needed if the transitive imports haven't already succeeded.
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


def _make_agent_config(instruction: str):
    """Create a minimal AgentConfig with the given instruction."""
    # Import types directly (not via kagent.adk) to avoid __init__ side effects
    import kagent.adk.types as types_mod

    return types_mod.AgentConfig(
        model={"type": "gemini", "model": "gemini-2.0-flash"},
        description="test agent",
        instruction=instruction,
    )


class TestInstructionEscaping:
    """Tests that curly braces in agent instructions are handled safely."""

    def test_instruction_with_curly_braces_creates_agent(self):
        """Agent with {repo} in instruction should not raise KeyError."""
        config = _make_agent_config(
            "Clone the repo {repo} and run tests on branch {branch}."
        )
        agent = config.to_agent("test_agent")
        assert agent is not None
        assert agent.name == "test_agent"

    def test_instruction_is_callable(self):
        """Instruction should be wrapped in a callable to bypass state injection."""
        config = _make_agent_config("Use {variable} in prompt")
        agent = config.to_agent("test_agent")
        # ADK's LlmAgent.instruction should be callable (InstructionProvider),
        # not a raw string, so bypass_state_injection is True.
        assert callable(agent.instruction)

    def test_instruction_callable_returns_original_text(self):
        """The instruction callable should return the original instruction text."""
        original = "Deploy to {environment} using {tool}"
        config = _make_agent_config(original)
        agent = config.to_agent("test_agent")
        # Call with a mock ReadonlyContext
        mock_ctx = MagicMock()
        result = agent.instruction(mock_ctx)
        assert result == original

    def test_instruction_without_braces_works(self):
        """Instructions without curly braces should still work normally."""
        config = _make_agent_config("Just a normal instruction without braces.")
        agent = config.to_agent("test_agent")
        assert callable(agent.instruction)
        mock_ctx = MagicMock()
        result = agent.instruction(mock_ctx)
        assert result == "Just a normal instruction without braces."

    def test_instruction_with_nested_braces(self):
        """Instructions with nested or multiple braces should be preserved."""
        original = "Format: {{key}}, single: {value}, mixed: {a} and {{b}}"
        config = _make_agent_config(original)
        agent = config.to_agent("test_agent")
        mock_ctx = MagicMock()
        result = agent.instruction(mock_ctx)
        assert result == original

    def test_instruction_with_json_like_content(self):
        """Instructions containing JSON-like content should be preserved."""
        original = 'Return output as JSON: {"status": "ok", "data": {items}}'
        config = _make_agent_config(original)
        agent = config.to_agent("test_agent")
        mock_ctx = MagicMock()
        result = agent.instruction(mock_ctx)
        assert result == original

    def test_empty_instruction(self):
        """Empty instruction should still work."""
        config = _make_agent_config("")
        agent = config.to_agent("test_agent")
        assert callable(agent.instruction)
        mock_ctx = MagicMock()
        result = agent.instruction(mock_ctx)
        assert result == ""
