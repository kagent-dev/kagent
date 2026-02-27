"""Tests for curly brace escaping in agent instructions.

Verifies that agent prompts containing curly braces (like {repo}) don't cause
KeyError from ADK's session state injection. See:
https://github.com/kagent-dev/kagent/issues/1382
"""

import asyncio
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
    # Import types via kagent.adk; __init__ side effects are neutralized by
    # the _isolate_types_import fixture stubbing heavy dependencies.
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
        config = _make_agent_config("Clone the repo {repo} and run tests on branch {branch}.")
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


class TestCanonicalInstructionBypass:
    """Tests that the callable instruction triggers bypass_state_injection in ADK.

    ADK's LlmAgent.canonical_instruction() returns (text, bypass_state_injection).
    When bypass_state_injection is True, inject_session_state is skipped —
    this is the mechanism that prevents KeyError on {repo}-style placeholders.
    """

    def test_callable_instruction_sets_bypass_true(self):
        """canonical_instruction should return bypass_state_injection=True for callable."""
        config = _make_agent_config("Clone {repo} and run tests")
        agent = config.to_agent("test_agent")
        mock_ctx = MagicMock()
        text, bypass = asyncio.get_event_loop().run_until_complete(
            agent.canonical_instruction(mock_ctx)
        )
        assert bypass is True, "callable instruction must set bypass_state_injection=True"
        assert text == "Clone {repo} and run tests"

    def test_raw_string_would_not_bypass(self):
        """A raw string instruction would set bypass_state_injection=False.

        This proves the fix is necessary: without wrapping instructions in a
        callable, ADK would attempt state injection and raise KeyError on
        unresolved {variables}.
        """
        config = _make_agent_config("Safe instruction without braces")
        agent = config.to_agent("test_agent")
        # Temporarily replace instruction with a raw string to verify ADK behavior
        agent.instruction = "Raw string with {repo}"
        mock_ctx = MagicMock()
        text, bypass = asyncio.get_event_loop().run_until_complete(
            agent.canonical_instruction(mock_ctx)
        )
        assert bypass is False, "raw string must set bypass_state_injection=False"
        assert text == "Raw string with {repo}"

    def test_inject_session_state_raises_on_unresolved_variable(self):
        """inject_session_state raises KeyError for {repo} when state is empty.

        This is the original bug: ADK's state injection treats {repo} as a
        context variable reference and raises KeyError when it's not in session
        state.  The callable wrapper prevents this path from executing.
        """
        from google.adk.utils.instructions_utils import inject_session_state

        mock_ctx = MagicMock()
        mock_ctx._invocation_context.session.state = {}
        with pytest.raises(KeyError, match="repo"):
            asyncio.get_event_loop().run_until_complete(
                inject_session_state("Clone {repo}", mock_ctx)
            )
