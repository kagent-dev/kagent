"""Tests for the HITL approval callback."""

from unittest.mock import MagicMock

from kagent.adk._approval import (
    APPROVAL_REQUIRED_STATUS,
    make_approval_callback,
    make_approval_key,
)


class MockState(dict):
    """Dict subclass that mimics ToolContext.state behavior."""
    pass


class MockToolContext:
    """Mock ToolContext for testing."""

    def __init__(self, state=None):
        self.state = MockState(state or {})


class MockBaseTool:
    """Mock BaseTool for testing."""

    def __init__(self, name: str):
        self.name = name


class TestMakeApprovalKey:
    """Tests for the make_approval_key helper."""

    def test_deterministic_key(self):
        """Same tool name and args produce the same key."""
        key1 = make_approval_key("delete_file", {"path": "/tmp/a"})
        key2 = make_approval_key("delete_file", {"path": "/tmp/a"})
        assert key1 == key2

    def test_different_args_produce_different_keys(self):
        """Different args produce different keys."""
        key1 = make_approval_key("delete_file", {"path": "/tmp/a"})
        key2 = make_approval_key("delete_file", {"path": "/tmp/b"})
        assert key1 != key2

    def test_different_tools_produce_different_keys(self):
        """Different tool names produce different keys."""
        key1 = make_approval_key("delete_file", {"path": "/tmp/a"})
        key2 = make_approval_key("write_file", {"path": "/tmp/a"})
        assert key1 != key2

    def test_key_starts_with_prefix(self):
        """Key has the expected prefix."""
        key = make_approval_key("delete_file", {"path": "/tmp"})
        assert key.startswith("_hitl_approved:delete_file:")

    def test_arg_order_independent(self):
        """Arg order doesn't affect the key (sorted internally)."""
        key1 = make_approval_key("tool", {"a": 1, "b": 2})
        key2 = make_approval_key("tool", {"b": 2, "a": 1})
        assert key1 == key2


class TestMakeApprovalCallback:
    """Tests for make_approval_callback."""

    def test_allows_non_approval_tools(self):
        """Tools not in the approval set proceed normally."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("read_file")
        ctx = MockToolContext()
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is None

    def test_blocks_approval_tools(self):
        """Tools in the approval set return the marker."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        ctx = MockToolContext()
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is not None
        assert result["status"] == APPROVAL_REQUIRED_STATUS
        assert result["tool"] == "delete_file"
        assert result["args"] == {"path": "/tmp"}

    def test_respects_prior_approval(self):
        """Tools with prior approval in state proceed normally."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        args = {"path": "/tmp"}
        approval_key = make_approval_key("delete_file", args)
        ctx = MockToolContext({approval_key: True})
        result = callback(tool, args, ctx)
        assert result is None

    def test_consumes_approval(self):
        """Approval is removed from state after use (one-time)."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        args = {"path": "/tmp"}
        approval_key = make_approval_key("delete_file", args)
        ctx = MockToolContext({approval_key: True})

        # First call: approval consumed, tool proceeds
        result = callback(tool, args, ctx)
        assert result is None
        assert approval_key not in ctx.state

        # Second call: no approval, tool blocked
        result = callback(tool, args, ctx)
        assert result is not None
        assert result["status"] == APPROVAL_REQUIRED_STATUS

    def test_multiple_tools_mixed(self):
        """Only tools in the set are blocked, others proceed."""
        callback = make_approval_callback({"delete_file", "write_file"})

        # read_file is not in the set
        read_tool = MockBaseTool("read_file")
        ctx = MockToolContext()
        assert callback(read_tool, {}, ctx) is None

        # delete_file is in the set
        delete_tool = MockBaseTool("delete_file")
        result = callback(delete_tool, {"path": "/tmp"}, ctx)
        assert result is not None
        assert result["status"] == APPROVAL_REQUIRED_STATUS

    def test_empty_approval_set_allows_all(self):
        """Empty approval set allows all tools."""
        callback = make_approval_callback(set())
        tool = MockBaseTool("delete_file")
        ctx = MockToolContext()
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is None
