"""Tests for the HITL approval callback and agent executor's HITL handling logic."""

import json
from unittest.mock import MagicMock

from a2a.types import DataPart, Message, Part, Role
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
from google.adk.sessions import Session
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.genai import types as genai_types

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._approval import make_approval_callback
from kagent.core.a2a import (
    KAGENT_ASK_USER_ANSWERS_KEY,
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
    KAGENT_HITL_DECISIONS_KEY,
    KAGENT_HITL_REJECTION_REASONS_KEY,
)


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
        """When tool_confirmation.confirmed is False, tool returns rejection string."""
        callback = make_approval_callback({"delete_file"})
        tool = MockBaseTool("delete_file")
        confirmation = ToolConfirmation(confirmed=False)
        ctx = MockToolContext(tool_confirmation=confirmation)
        result = callback(tool, {"path": "/tmp"}, ctx)
        assert isinstance(result, str)
        assert "rejected" in result

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


class MockFunctionResponse:
    def __init__(self, name, id):
        self.name = name
        self.id = id


class MockFunctionCall:
    def __init__(self, name, id, args=None):
        self.name = name
        self.id = id
        self.args = args or {}


class MockEvent:
    def __init__(self, function_calls=None, function_responses=None):
        self._function_calls = function_calls or []
        self._function_responses = function_responses or []

    def get_function_calls(self):
        return self._function_calls

    def get_function_responses(self):
        return self._function_responses


def test_find_pending_confirmations_empty():
    session = MagicMock(spec=Session)
    session.events = []
    pending = A2aAgentExecutor._find_pending_confirmations(session)
    assert pending == {}


def test_find_pending_confirmations_no_confirmations():
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[MockFunctionCall("other_function", "fc1")],
            function_responses=[MockFunctionResponse("other_function", "fc1")],
        )
    ]
    pending = A2aAgentExecutor._find_pending_confirmations(session)
    assert pending == {}


def test_find_pending_confirmations_with_pending():
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                )
            ]
        )
    ]
    pending = A2aAgentExecutor._find_pending_confirmations(session)
    assert pending == {"fc1": "orig123"}


def test_find_pending_confirmations_with_responded():
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                )
            ]
        ),
        MockEvent(function_responses=[MockFunctionResponse(REQUEST_CONFIRMATION_FUNCTION_CALL_NAME, "fc1")]),
    ]
    pending = A2aAgentExecutor._find_pending_confirmations(session)
    assert pending == {}


def test_find_pending_confirmations_missing_original_id():
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={},
                )
            ]
        )
    ]
    pending = A2aAgentExecutor._find_pending_confirmations(session)
    assert pending == {"fc1": None}


def _make_simple_message(parts=None) -> Message:
    """Create a minimal real Message for testing."""
    return Message(
        role=Role.user,
        message_id="test-msg",
        task_id="test-task",
        context_id="test-ctx",
        parts=parts or [],
    )


def test_process_hitl_decision_no_pending():
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = []

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_APPROVE, _make_simple_message())
    assert parts is None


def test_process_hitl_decision_uniform_approve():
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                )
            ]
        )
    ]

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_APPROVE, _make_simple_message())

    assert parts is not None
    assert len(parts) == 1
    fr = parts[0].function_response
    assert fr.name == REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
    assert fr.id == "fc1"

    resp = json.loads(fr.response["response"])
    assert resp["confirmed"] is True


def test_process_hitl_decision_uniform_deny():
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                )
            ]
        )
    ]

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_REJECT, _make_simple_message())

    assert parts is not None
    assert len(parts) == 1
    fr = parts[0].function_response

    resp = json.loads(fr.response["response"])
    assert resp["confirmed"] is False


def test_process_hitl_decision_batch():
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                ),
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc2",
                    args={"originalFunctionCall": {"id": "orig456"}},
                ),
            ]
        )
    ]

    message = Message(
        role=Role.user,
        message_id="msg1",
        task_id="task1",
        context_id="ctx1",
        parts=[
            Part(
                DataPart(
                    data={
                        KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                        KAGENT_HITL_DECISIONS_KEY: {
                            "orig123": KAGENT_HITL_DECISION_TYPE_APPROVE,
                            "orig456": KAGENT_HITL_DECISION_TYPE_REJECT,
                        },
                    }
                )
            )
        ],
    )

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_BATCH, message)

    assert parts is not None
    assert len(parts) == 2

    parts_by_id = {p.function_response.id: p.function_response for p in parts}

    fr1 = parts_by_id["fc1"]
    resp1 = json.loads(fr1.response["response"])
    assert resp1["confirmed"] is True

    fr2 = parts_by_id["fc2"]
    resp2 = json.loads(fr2.response["response"])
    assert resp2["confirmed"] is False


def test_process_hitl_decision_uniform_deny_with_reason():
    """Uniform deny with a rejection_reason populates ToolConfirmation.payload."""
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                )
            ]
        )
    ]

    message = Message(
        role=Role.user,
        message_id="msg1",
        task_id="task1",
        context_id="ctx1",
        parts=[
            Part(
                DataPart(
                    data={
                        KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_REJECT,
                        "rejection_reason": "Too risky",
                    }
                )
            )
        ],
    )

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_REJECT, message)

    assert parts is not None
    assert len(parts) == 1
    fr = parts[0].function_response
    resp = json.loads(fr.response["response"])
    assert resp["confirmed"] is False
    assert resp["payload"]["rejection_reason"] == "Too risky"


def test_process_hitl_decision_batch_with_per_tool_reason():
    """Batch deny with per-tool rejection reasons populates ToolConfirmation.payload for denied tools."""
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "orig123"}},
                ),
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc2",
                    args={"originalFunctionCall": {"id": "orig456"}},
                ),
            ]
        )
    ]

    message = Message(
        role=Role.user,
        message_id="msg1",
        task_id="task1",
        context_id="ctx1",
        parts=[
            Part(
                DataPart(
                    data={
                        KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                        KAGENT_HITL_DECISIONS_KEY: {
                            "orig123": KAGENT_HITL_DECISION_TYPE_APPROVE,
                            "orig456": KAGENT_HITL_DECISION_TYPE_REJECT,
                        },
                        KAGENT_HITL_REJECTION_REASONS_KEY: {
                            "orig456": "Wrong environment",
                        },
                    }
                )
            )
        ],
    )

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_BATCH, message)

    assert parts is not None
    assert len(parts) == 2

    parts_by_id = {p.function_response.id: p.function_response for p in parts}

    # Approved tool — no payload
    fr1 = parts_by_id["fc1"]
    resp1 = json.loads(fr1.response["response"])
    assert resp1["confirmed"] is True
    assert resp1.get("payload") is None

    # Denied tool — reason in payload
    fr2 = parts_by_id["fc2"]
    resp2 = json.loads(fr2.response["response"])
    assert resp2["confirmed"] is False
    assert resp2["payload"]["rejection_reason"] == "Wrong environment"


def test_approval_callback_rejection_with_reason():
    """Rejected callback with a reason in payload returns a result containing that reason."""
    callback = make_approval_callback({"delete_file"})
    tool = MockBaseTool("delete_file")
    confirmation = ToolConfirmation(confirmed=False, payload={"rejection_reason": "Dangerous path"})
    ctx = MockToolContext(tool_confirmation=confirmation)
    result = callback(tool, {"path": "/tmp"}, ctx)
    assert result is not None
    assert "Dangerous path" in result


def test_approval_callback_rejection_without_reason():
    """Rejected callback without a reason returns generic rejection message in result key."""
    callback = make_approval_callback({"delete_file"})
    tool = MockBaseTool("delete_file")
    confirmation = ToolConfirmation(confirmed=False)
    ctx = MockToolContext(tool_confirmation=confirmation)
    result = callback(tool, {"path": "/tmp"}, ctx)
    assert result is not None
    assert result == "Tool call was rejected by user."


# ---------------------------------------------------------------------------
# Ask-user tests
# ---------------------------------------------------------------------------


def test_process_hitl_decision_ask_user_answers():
    """Ask-user answers produce an approved ToolConfirmation with answers payload."""
    executor = A2aAgentExecutor(runner=MagicMock())
    session = MagicMock(spec=Session)
    session.events = [
        MockEvent(
            function_calls=[
                MockFunctionCall(
                    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                    "fc1",
                    args={"originalFunctionCall": {"id": "ask123"}},
                )
            ]
        )
    ]

    answers = [{"answer": ["PostgreSQL"]}, {"answer": ["Auth", "Caching"]}]
    message = Message(
        role=Role.user,
        message_id="msg1",
        task_id="task1",
        context_id="ctx1",
        parts=[
            Part(
                DataPart(
                    data={
                        KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                        KAGENT_ASK_USER_ANSWERS_KEY: answers,
                    }
                )
            )
        ],
    )

    parts = executor._process_hitl_decision(session, KAGENT_HITL_DECISION_TYPE_APPROVE, message)

    assert parts is not None
    assert len(parts) == 1
    fr = parts[0].function_response
    resp = json.loads(fr.response["response"])
    assert resp["confirmed"] is True
    assert resp["payload"]["answers"] == answers
