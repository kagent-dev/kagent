"""Tests for GrepFileTool's timeout protection against slow/catastrophic regex matching."""

from unittest.mock import patch

import pytest
from kagent.skills import initialize_session_path

from kagent.adk.tools.file_tools import GrepFileTool


class MockSession:
    def __init__(self, session_id: str = "test-session-grep-timeout"):
        self.id = session_id


class MockToolContext:
    def __init__(self, session_id: str = "test-session-grep-timeout"):
        self.session = MockSession(session_id)


@pytest.mark.asyncio
async def test_grep_file_tool_times_out_on_slow_match(tmp_path):
    skills_dir = tmp_path / "skills"
    skills_dir.mkdir()
    session_id = "test-session-grep-timeout"
    initialize_session_path(session_id, str(skills_dir))

    tool = GrepFileTool(skills_directory=str(skills_dir))
    tool._TIMEOUT_SECONDS = 0.05

    def slow_grep(*args, **kwargs):
        import time

        time.sleep(1)
        return "no matches found"

    with patch("kagent.adk.tools.file_tools.grep_content", side_effect=slow_grep):
        result = await tool.run_async(
            args={"pattern": "foo", "path": "."},
            tool_context=MockToolContext(session_id),
        )

    assert "took too long" in result


@pytest.mark.asyncio
async def test_grep_file_tool_returns_normally_when_fast(tmp_path):
    skills_dir = tmp_path / "skills"
    skills_dir.mkdir()
    session_id = "test-session-grep-timeout-fast"
    initialize_session_path(session_id, str(skills_dir))

    tool = GrepFileTool(skills_directory=str(skills_dir))

    with patch("kagent.adk.tools.file_tools.grep_content", return_value="match found") as mocked:
        result = await tool.run_async(
            args={"pattern": "foo", "path": "."},
            tool_context=MockToolContext(session_id),
        )

    assert result == "match found"
    assert mocked.called
