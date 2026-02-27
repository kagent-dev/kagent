"""Tests for curly brace handling in agent instructions.

Verifies that agent prompts containing curly braces (like {repo}) don't cause
KeyError from ADK's session state injection, by using static_instruction which
bypasses placeholder processing entirely. See:
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
    import kagent.adk.types as types_mod

    return types_mod.AgentConfig(
        model={"type": "gemini", "model": "gemini-2.0-flash"},
        description="test agent",
        instruction=instruction,
    )


class TestStaticInstruction:
    """Tests that curly braces in agent instructions are handled via static_instruction."""

    def test_instruction_with_curly_braces_creates_agent(self):
        """Agent with {repo} in instruction should not raise KeyError."""
        config = _make_agent_config("Clone the repo {repo} and run tests on branch {branch}.")
        agent = config.to_agent("test_agent")
        assert agent is not None
        assert agent.name == "test_agent"

    def test_instruction_goes_to_static_instruction(self):
        """Instruction should be set as static_instruction to bypass state injection."""
        original = "Use {variable} in prompt"
        config = _make_agent_config(original)
        agent = config.to_agent("test_agent")
        assert agent.static_instruction == original

    def test_dynamic_instruction_is_empty_without_memory(self):
        """Without memory, the dynamic instruction field should not be set."""
        config = _make_agent_config("Deploy to {environment}")
        agent = config.to_agent("test_agent")
        assert not agent.instruction

    def test_instruction_without_braces_works(self):
        """Instructions without curly braces should still work normally."""
        config = _make_agent_config("Just a normal instruction without braces.")
        agent = config.to_agent("test_agent")
        assert agent.static_instruction == "Just a normal instruction without braces."

    def test_instruction_with_nested_braces(self):
        """Instructions with nested or multiple braces should be preserved."""
        original = "Format: {{key}}, single: {value}, mixed: {a} and {{b}}"
        config = _make_agent_config(original)
        agent = config.to_agent("test_agent")
        assert agent.static_instruction == original

    def test_instruction_with_json_like_content(self):
        """Instructions containing JSON-like content should be preserved."""
        original = 'Return output as JSON: {"status": "ok", "data": {items}}'
        config = _make_agent_config(original)
        agent = config.to_agent("test_agent")
        assert agent.static_instruction == original

    def test_empty_instruction(self):
        """Empty instruction should still work."""
        config = _make_agent_config("")
        agent = config.to_agent("test_agent")
        assert agent.static_instruction == ""


class TestStaticInstructionBypass:
    """Tests that static_instruction bypasses inject_session_state in ADK.

    ADK's _build_instructions() passes static_instruction directly to
    _transformers.t_content() without any variable substitution, while
    the instruction field goes through _process_agent_instruction() which
    calls inject_session_state(). This is the mechanism that prevents
    KeyError on {repo}-style placeholders.
    """

    def test_inject_session_state_raises_on_unresolved_variable(self):
        """inject_session_state raises KeyError for {repo} when state is empty.

        This is the original bug: ADK's state injection treats {repo} as a
        context variable reference and raises KeyError when it's not in session
        state. Using static_instruction prevents this path from executing.
        """
        from google.adk.utils.instructions_utils import inject_session_state

        mock_ctx = MagicMock()
        mock_ctx._invocation_context.session.state = {}
        with pytest.raises(KeyError, match="repo"):
            asyncio.get_event_loop().run_until_complete(inject_session_state("Clone {repo}", mock_ctx))

    def test_raw_string_instruction_would_not_bypass(self):
        """A raw string in the instruction field would set bypass_state_injection=False.

        This proves the fix is necessary: without static_instruction, ADK
        would attempt state injection on the instruction field and raise
        KeyError on unresolved {variables}.
        """
        config = _make_agent_config("Safe instruction")
        agent = config.to_agent("test_agent")
        # Temporarily set instruction to a raw string to verify ADK behavior
        agent.instruction = "Raw string with {repo}"
        mock_ctx = MagicMock()
        text, bypass = asyncio.get_event_loop().run_until_complete(agent.canonical_instruction(mock_ctx))
        assert bypass is False, "raw string in instruction must set bypass_state_injection=False"
        assert text == "Raw string with {repo}"
