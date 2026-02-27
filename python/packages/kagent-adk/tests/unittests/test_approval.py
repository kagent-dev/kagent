"""Tests for the HITL approval callback (ADK-native request_confirmation)."""

from unittest.mock import MagicMock

from google.adk.tools.tool_confirmation import ToolConfirmation

from kagent.adk._approval import make_approval_callback


class MockState(dict):
    """Dict subclass that mimics ToolContext.state behavior."""

    pass


class MockEventActions:
    """Mock EventActions for testing."""

    def __init__(self):
        self.requested_tool_confirmations: dict[str, ToolConfirmation] = {}


class MockToolContext:
    """Mock ToolContext for testing."""

    def __init__(self, tool_confirmation=None):
        self.state = MockState()
        self.function_call_id = "test_fc_id"
        self._event_actions = MockEventActions()
        self.tool_confirmation = tool_confirmation

    def request_confirmation(self, *, hint=None, payload=None):
        """Mimics ToolContext.request_confirmation()."""
        self._event_actions.requested_tool_confirmations[self.function_call_id] = ToolConfirmation(
            hint=hint, payload=payload
        )


class MockBaseTool:
    """Mock BaseTool for testing."""

    def __init__(self, name: str):
        self.name = name


class TestMakeApprovalCallback:
    """Tests for make_approval_callback with ADK-native request_confirmation."""

    def test_allows_non_approval_tools(self):
        """Tools not in the approval set proceed normally."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("read_file")
        ctx = MockToolContext()
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is None
        # No confirmation requested
        assert len(ctx._event_actions.requested_tool_confirmations) == 0

    def test_blocks_approval_tools_and_requests_confirmation(self):
        """Tools in the approval set request confirmation and return a blocking dict."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        ctx = MockToolContext()
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is not None
        assert result["status"] == "confirmation_requested"
        assert result["tool"] == "delete_file"
        # Confirmation should be stored in event_actions
        assert "test_fc_id" in ctx._event_actions.requested_tool_confirmations

    def test_approved_confirmation_allows_execution(self):
        """When tool_confirmation.confirmed is True, tool proceeds."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        confirmation = ToolConfirmation(confirmed=True)
        ctx = MockToolContext(tool_confirmation=confirmation)
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is None  # Tool proceeds

    def test_rejected_confirmation_blocks_execution(self):
        """When tool_confirmation.confirmed is False, tool returns rejection."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        confirmation = ToolConfirmation(confirmed=False)
        ctx = MockToolContext(tool_confirmation=confirmation)
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is not None
        assert result["status"] == "rejected"

    def test_multiple_tools_mixed(self):
        """Only tools in the set request confirmation, others proceed."""
        callback = make_approval_callback({"delete_file", "write_file"})

        # read_file is not in the set
        read_tool = MockBaseTool("read_file")
        ctx = MockToolContext()
        assert callback(read_tool, {}, ctx) is None

        # delete_file is in the set — blocks
        delete_tool = MockBaseTool("delete_file")
        ctx2 = MockToolContext()
        result = callback(delete_tool, {"path": "/tmp"}, ctx2)
        assert result is not None
        assert result["status"] == "confirmation_requested"

    def test_empty_approval_set_allows_all(self):
        """Empty approval set allows all tools."""
        callback = make_approval_callback(set())
        tool = MockBaseTool("delete_file")
        ctx = MockToolContext()
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert result is None

    def test_hint_contains_tool_name(self):
        """The confirmation hint mentions the tool name."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        ctx = MockToolContext()
        callback(tool, {"path": "/tmp"}, ctx)
        confirmation = ctx._event_actions.requested_tool_confirmations["test_fc_id"]
        assert "delete_file" in confirmation.hint

    def test_non_approval_tool_with_confirmation_still_proceeds(self):
        """If a non-approval tool somehow has tool_confirmation set, it still proceeds."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("read_file")  # Not in approval set
        confirmation = ToolConfirmation(confirmed=True)
        ctx = MockToolContext(tool_confirmation=confirmation)
        result = callback(tool, {}, ctx)
        assert result is None
