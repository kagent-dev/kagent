"""Tests for bash tool."""

import asyncio

import pytest
from agents.exceptions import UserError

from kagent.openai.agents.tools import SRT_SHELL_TOOL


class TestBashTool:
    """Tests for bash tool."""

    def test_bash_success(self):
        """Test executing a bash command successfully."""
        result = asyncio.run(
            SRT_SHELL_TOOL.on_invoke_tool(None, '{"command": "echo Hello, World!"}')  # type: ignore
        )

        assert "Exit code: 0" in result
        assert "Hello, World!" in result

    def test_bash_error(self):
        """Test executing a bash command that fails."""
        result = asyncio.run(
            SRT_SHELL_TOOL.on_invoke_tool(None, '{"command": "ls /nonexistent"}')  # type: ignore
        )

        # Should return error in the output (not raise)
        assert "Exit code:" in result
        assert "cannot access" in result.lower() or "no such file" in result.lower()

    def test_bash_chained_commands(self):
        """Test executing chained bash commands."""
        result = asyncio.run(
            SRT_SHELL_TOOL.on_invoke_tool(
                None,  # type: ignore
                '{"command": "echo first && echo second"}',
            )
        )

        assert "Exit code: 0" in result
        assert "first" in result
        assert "second" in result
