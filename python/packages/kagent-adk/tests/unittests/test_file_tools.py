"""Tests for GrepFileTool: timeout protection, and fidelity of the search pattern."""

import asyncio
import os
from pathlib import Path
from unittest.mock import patch

import pytest
from kagent.skills import get_session_path, initialize_session_path

from kagent.adk.tools.file_tools import GrepFileTool, _run_with_deadline


class MockSession:
    def __init__(self, session_id: str = "test-session-grep-timeout"):
        self.id = session_id


class MockToolContext:
    def __init__(self, session_id: str = "test-session-grep-timeout"):
        self.session = MockSession(session_id)


# A pattern/subject pair that makes Python's backtracking `re` engine explore
# an astronomical number of ways to split the run of "a"s, none of which can
# match because the line ends in "!". It does not finish in any practical time.
CATASTROPHIC_PATTERN = r"(a+)+$"
CATASTROPHIC_SUBJECT = "a" * 32 + "!"


def _worker_pid() -> int:
    """Module-level so it is picklable to a spawned worker."""
    return os.getpid()


@pytest.mark.asyncio
async def test_grep_work_runs_in_a_separate_process():
    """Fast tripwire: the work must leave this process.

    `re` holds the GIL while matching, so running a match in a *thread* makes
    run_async's timeout unenforceable -- the event loop cannot run to fire it.
    The behavioral test below proves that properly but *hangs* if this
    regresses, and there is no pytest-timeout plugin here to cut it short.
    This returns in about 100ms instead, so a regression names itself.

    Asserting on the PID rather than the executor type states the property
    that actually matters, and stays true however the offload is implemented.
    """
    assert await _run_with_deadline(_worker_pid, 30) != os.getpid()


@pytest.mark.asyncio
async def test_grep_file_tool_times_out_on_catastrophic_backtracking(tmp_path):
    """The timeout must hold against a match that never yields the GIL.

    The previous version of this test faked slowness with `time.sleep`, which
    *releases* the GIL -- so it passed while the real hazard went unguarded.
    This one runs a genuinely pathological match, and additionally asserts the
    event loop kept running throughout: that is what a thread pool cannot do,
    and it is the difference between a timeout that fires and one that cannot.
    """
    skills_dir = tmp_path / "skills"
    skills_dir.mkdir()
    session_id = "test-session-grep-catastrophic"
    initialize_session_path(session_id, str(skills_dir))

    working_dir = Path(get_session_path(session_id=session_id))
    (working_dir / "evil.txt").write_text(CATASTROPHIC_SUBJECT + "\n")

    tool = GrepFileTool(skills_directory=str(skills_dir))
    # Comfortably above worker spawn cost, low enough to keep the test quick.
    tool._TIMEOUT_SECONDS = 3

    ticks = 0

    async def heartbeat():
        nonlocal ticks
        while True:
            await asyncio.sleep(0.05)
            ticks += 1

    beat = asyncio.create_task(heartbeat())
    try:
        result = await tool.run_async(
            args={"pattern": CATASTROPHIC_PATTERN, "path": "evil.txt"},
            tool_context=MockToolContext(session_id),
        )
    finally:
        beat.cancel()

    assert "took too long" in result
    # If the match had run in a thread, the loop would have been frozen and
    # this would be 0 -- assuming the timeout fired at all.
    assert ticks > 5, f"event loop was starved during the match (ticks={ticks})"


def _session_with_file(tmp_path, session_id: str, name: str, content: str):
    """Build a session working dir containing one file, and return the tool.

    Tests here use real files rather than patching grep_content: the search
    runs in a worker *process*, so a MagicMock cannot reach it (it is not
    picklable, and the child imports the real module regardless).
    """
    skills_dir = tmp_path / "skills"
    skills_dir.mkdir()
    initialize_session_path(session_id, str(skills_dir))
    working_dir = Path(get_session_path(session_id=session_id))
    (working_dir / name).write_text(content)
    return GrepFileTool(skills_directory=str(skills_dir))


@pytest.mark.asyncio
async def test_grep_file_tool_returns_matches_for_a_normal_pattern(tmp_path):
    session_id = "test-session-grep-fast"
    tool = _session_with_file(tmp_path, session_id, "data.txt", "hello foo\nunrelated\n")

    result = await tool.run_async(
        args={"pattern": "foo", "path": "data.txt"},
        tool_context=MockToolContext(session_id),
    )

    assert "data.txt:1:hello foo" in result
    assert "unrelated" not in result


@pytest.mark.asyncio
async def test_grep_file_tool_passes_the_pattern_through_verbatim(tmp_path):
    """Whitespace is meaningful in a regex, so the pattern must not be stripped.

    Stripping turns `"foo "` into `"foo"`, which matches "foobar" -- a string
    the user's pattern explicitly excludes. The Go runtime trims only for its
    empty check and searches with the raw pattern (skills.go), so stripping
    here would also split the two runtimes on identical input.
    """
    session_id = "test-session-grep-pattern"
    # "foobar" is the discriminator: the pattern "foo " must NOT match it,
    # while a stripped "foo" would. So the assertion fails loudly if the
    # trailing space is trimmed anywhere between here and re.search.
    tool = _session_with_file(tmp_path, session_id, "data.txt", "foobar\nfoo bar\n")

    result = await tool.run_async(
        args={"pattern": "foo ", "path": "data.txt"},
        tool_context=MockToolContext(session_id),
    )

    assert "data.txt:2:foo bar" in result
    # Line 1 is "foobar": a stripped "foo" would match it, the real pattern must not.
    assert ":1:" not in result


@pytest.mark.asyncio
async def test_grep_file_tool_still_rejects_a_whitespace_only_pattern(tmp_path):
    """Keeping the pattern raw must not weaken the empty-pattern guard."""
    skills_dir = tmp_path / "skills"
    skills_dir.mkdir()
    session_id = "test-session-grep-blank"
    initialize_session_path(session_id, str(skills_dir))

    tool = GrepFileTool(skills_directory=str(skills_dir))

    with patch("kagent.adk.tools.file_tools.grep_content") as mocked:
        result = await tool.run_async(
            args={"pattern": "   ", "path": "."},
            tool_context=MockToolContext(session_id),
        )

    assert "No pattern provided" in result
    assert not mocked.called
