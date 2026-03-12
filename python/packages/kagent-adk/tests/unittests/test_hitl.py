"""Tests for the HITL approval callback, agent executor's HITL handling, and HitlAwareAgentTool."""

import asyncio
import json
from unittest.mock import AsyncMock, MagicMock, create_autospec, patch

from a2a.types import DataPart, Message, Part, Role, TaskState, TaskStatus, TextPart
from google.adk.flows.llm_flows.functions import REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
from google.adk.sessions import Session
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.genai import types as genai_types

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._approval import make_approval_callback
from kagent.adk._hitl_agent_tool import (
    HitlAwareAgentTool,
    _collect_text_parts,
    _extract_error_text,
    _extract_hitl_hint,
    _extract_input_required,
    _save_hitl_state,
    _send_and_collect,
)
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

    def to_dict(self):
        return dict(self)


class MockEventActions:
    """Mock EventActions for testing."""

    def __init__(self):
        self.requested_tool_confirmations: dict[str, ToolConfirmation] = {}
        self.skip_summarization = False


class MockToolContext:
    """Mock ToolContext for testing."""

    def __init__(self, tool_confirmation=None):
        self.state = MockState()
        self.function_call_id = "test_fc_id"
        self._event_actions = MockEventActions()
        self.actions = self._event_actions
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


# ---------------------------------------------------------------------------
# HitlAwareAgentTool helper tests
# ---------------------------------------------------------------------------


class MockADKEvent:
    """Minimal mock for an ADK Event with custom_metadata."""

    def __init__(self, custom_metadata=None, author=None, partial=False):
        self.custom_metadata = custom_metadata
        self.author = author
        self.partial = partial


class TestExtractInputRequired:
    """Tests for _extract_input_required."""

    def test_returns_none_for_no_metadata(self):
        event = MockADKEvent(custom_metadata=None)
        assert _extract_input_required(event) is None

    def test_returns_none_for_no_a2a_response(self):
        event = MockADKEvent(custom_metadata={"some_key": "value"})
        assert _extract_input_required(event) is None

    def test_returns_none_for_non_dict_response(self):
        event = MockADKEvent(custom_metadata={"a2a:response": "not a dict"})
        assert _extract_input_required(event) is None

    def test_returns_none_for_working_state(self):
        response = {"status": {"state": "working"}}
        event = MockADKEvent(custom_metadata={"a2a:response": response})
        assert _extract_input_required(event) is None

    def test_returns_none_for_completed_state(self):
        response = {"status": {"state": "completed"}}
        event = MockADKEvent(custom_metadata={"a2a:response": response})
        assert _extract_input_required(event) is None

    def test_returns_response_for_input_required(self):
        response = {
            "id": "task-123",
            "contextId": "ctx-456",
            "status": {
                "state": "input-required",
                "message": {
                    "messageId": "msg-1",
                    "role": "agent",
                    "parts": [{"kind": "text", "text": "Approval needed"}],
                },
            },
        }
        event = MockADKEvent(custom_metadata={"a2a:response": response})
        result = _extract_input_required(event)
        assert result is not None
        assert result["id"] == "task-123"
        assert result["status"]["state"] == "input-required"


class TestExtractHitlHint:
    """Tests for _extract_hitl_hint."""

    def test_extracts_text_from_parts(self):
        response = {
            "status": {
                "state": "input-required",
                "message": {
                    "parts": [{"text": "Approve this tool?"}],
                },
            },
        }
        assert _extract_hitl_hint(response) == "Approve this tool?"

    def test_returns_fallback_for_no_message(self):
        response = {"status": {"state": "input-required"}}
        assert _extract_hitl_hint(response) == "Sub-agent requires user input."

    def test_returns_fallback_for_empty_parts(self):
        response = {"status": {"state": "input-required", "message": {"parts": []}}}
        assert _extract_hitl_hint(response) == "Sub-agent requires user input."


class TestSaveHitlState:
    """Tests for _save_hitl_state."""

    def test_saves_task_and_context_ids(self):
        tool_context = MockToolContext()
        a2a_response = {"id": "task-42", "contextId": "ctx-99"}
        _save_hitl_state(tool_context, a2a_response)
        state = tool_context.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY]
        assert state["task_id"] == "task-42"
        assert state["context_id"] == "ctx-99"

    def test_handles_snake_case_context_id(self):
        tool_context = MockToolContext()
        a2a_response = {"id": "task-1", "context_id": "ctx-2"}
        _save_hitl_state(tool_context, a2a_response)
        state = tool_context.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY]
        assert state["context_id"] == "ctx-2"


class TestCollectTextParts:
    """Tests for _collect_text_parts."""

    def test_collects_text_from_message(self):
        message = MagicMock()
        text_part = MagicMock()
        text_part.root = TextPart(text="hello")
        message.parts = [text_part]
        result = []
        _collect_text_parts(message, result)
        assert result == ["hello"]

    def test_handles_none_message(self):
        result = []
        _collect_text_parts(None, result)
        assert result == []


class TestExtractErrorText:
    """Tests for _extract_error_text."""

    def test_extracts_error_from_message(self):
        message = MagicMock()
        text_part = MagicMock()
        text_part.root = TextPart(text="Something failed")
        message.parts = [text_part]
        assert _extract_error_text(message) == "Something failed"

    def test_returns_default_for_no_message(self):
        assert _extract_error_text(None) == "Sub-agent execution failed"


class TestHitlAwareAgentToolRejection:
    """Tests for HitlAwareAgentTool._handle_rejection."""

    def test_rejection_clears_state_and_returns_message(self):
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        tool = HitlAwareAgentTool(agent=agent)

        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=False))
        ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {"task_id": "t1", "context_id": "c1"}

        result = tool._handle_rejection(ctx)
        assert "rejected" in result
        assert HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY not in ctx.state

    def test_rejection_includes_reason(self):
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        tool = HitlAwareAgentTool(agent=agent)

        ctx = MockToolContext(
            tool_confirmation=ToolConfirmation(
                confirmed=False,
                payload={"rejection_reason": "Too dangerous"},
            )
        )
        ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {"task_id": "t1", "context_id": "c1"}

        result = tool._handle_rejection(ctx)
        assert "Too dangerous" in result


class TestHitlAwareAgentToolForwardAndContinue:
    """Tests for HitlAwareAgentTool._forward_and_continue."""

    def test_missing_hitl_state_returns_error(self):
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        tool = HitlAwareAgentTool(agent=agent)

        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=True))
        # No HITL state saved

        result = asyncio.get_event_loop().run_until_complete(tool._forward_and_continue(ctx))
        assert "error" in result

    def test_missing_ensure_resolved_returns_error(self):
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        # Remove both resolve methods
        del agent.ensure_resolved
        del agent._ensure_resolved
        tool = HitlAwareAgentTool(agent=agent)

        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=True))
        ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {"task_id": "t1", "context_id": "c1"}

        result = asyncio.get_event_loop().run_until_complete(tool._forward_and_continue(ctx))
        assert "error" in result
        # State should be cleared
        assert HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY not in ctx.state

    def test_successful_forward_returns_text(self):
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        agent._ensure_resolved = AsyncMock()

        # Mock A2A client with a completed response
        mock_task = MagicMock()
        mock_task.status = TaskStatus(
            state=TaskState.completed,
            message=Message(
                message_id="m1",
                role=Role.agent,
                parts=[Part(TextPart(text="Task completed successfully"))],
            ),
        )

        async def mock_send_message(**kwargs):
            yield (mock_task, None)

        agent._a2a_client = MagicMock()
        agent._a2a_client.send_message = mock_send_message

        tool = HitlAwareAgentTool(agent=agent)

        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=True))
        ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {"task_id": "t1", "context_id": "c1"}

        result = asyncio.get_event_loop().run_until_complete(tool._forward_and_continue(ctx))
        assert result == "Task completed successfully"
        # State should be cleared after success
        assert HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY not in ctx.state

    def test_timeout_returns_error_and_clears_state(self):
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        agent._ensure_resolved = AsyncMock()

        async def slow_send_message(**kwargs):
            await asyncio.sleep(10)
            yield  # pragma: no cover

        agent._a2a_client = MagicMock()
        agent._a2a_client.send_message = slow_send_message

        tool = HitlAwareAgentTool(agent=agent)
        tool._FORWARD_TIMEOUT_SECONDS = 0.01  # Very short for testing

        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=True))
        ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {"task_id": "t1", "context_id": "c1"}

        result = asyncio.get_event_loop().run_until_complete(tool._forward_and_continue(ctx))
        assert "error" in result
        assert "Timed out" in result["error"]
        assert HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY not in ctx.state

    def test_multi_round_hitl_re_requests_confirmation(self):
        """When subagent enters input_required again, tool re-requests confirmation."""
        from google.adk.agents.remote_a2a_agent import RemoteA2aAgent

        agent = create_autospec(RemoteA2aAgent, instance=True)
        agent.name = "test_agent"
        agent.description = "test"
        agent._ensure_resolved = AsyncMock()

        # Sub-agent enters input_required again
        mock_task = MagicMock()
        mock_task.status = TaskStatus(
            state=TaskState.input_required,
            message=Message(
                message_id="m2",
                role=Role.agent,
                parts=[Part(TextPart(text="Need more approval"))],
            ),
        )
        mock_task.model_dump = MagicMock(
            return_value={
                "id": "task-2",
                "contextId": "ctx-2",
                "status": {
                    "state": "input-required",
                    "message": {
                        "parts": [{"text": "Need more approval"}],
                    },
                },
            }
        )

        async def mock_send_message(**kwargs):
            yield (mock_task, None)

        agent._a2a_client = MagicMock()
        agent._a2a_client.send_message = mock_send_message

        tool = HitlAwareAgentTool(agent=agent)

        ctx = MockToolContext(tool_confirmation=ToolConfirmation(confirmed=True))
        ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {"task_id": "t1", "context_id": "c1"}

        result = asyncio.get_event_loop().run_until_complete(tool._forward_and_continue(ctx))
        # Should return confirmation_requested
        assert result["status"] == "confirmation_requested"
        assert result["subagent_hitl"] is True
        # Should have re-requested confirmation
        assert "test_fc_id" in ctx._event_actions.requested_tool_confirmations
        # State should be updated with new task/context IDs
        new_state = ctx.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY]
        assert new_state["task_id"] == "task-2"


class TestSendAndCollect:
    """Tests for _send_and_collect."""

    def test_completed_task(self):
        mock_task = MagicMock()
        mock_task.status = TaskStatus(
            state=TaskState.completed,
            message=Message(
                message_id="m1",
                role=Role.agent,
                parts=[Part(TextPart(text="Done"))],
            ),
        )

        async def mock_send(**kwargs):
            yield (mock_task, None)

        ctx = MockToolContext()
        result_text, needs_input, response_dict = asyncio.get_event_loop().run_until_complete(
            _send_and_collect(mock_send, MagicMock(), ctx)
        )
        assert result_text == "Done"
        assert needs_input is False
        assert response_dict is None

    def test_failed_task(self):
        mock_task = MagicMock()
        mock_task.status = TaskStatus(
            state=TaskState.failed,
            message=Message(
                message_id="m1",
                role=Role.agent,
                parts=[Part(TextPart(text="Crash!"))],
            ),
        )

        async def mock_send(**kwargs):
            yield (mock_task, None)

        ctx = MockToolContext()
        result_text, needs_input, response_dict = asyncio.get_event_loop().run_until_complete(
            _send_and_collect(mock_send, MagicMock(), ctx)
        )
        assert result_text == "Crash!"
        assert needs_input is False

    def test_input_required_task(self):
        mock_task = MagicMock()
        mock_task.status = TaskStatus(
            state=TaskState.input_required,
            message=Message(
                message_id="m1",
                role=Role.agent,
                parts=[Part(TextPart(text="Need approval"))],
            ),
        )
        mock_task.model_dump = MagicMock(return_value={"id": "t1", "status": {"state": "input-required"}})

        async def mock_send(**kwargs):
            yield (mock_task, None)

        ctx = MockToolContext()
        result_text, needs_input, response_dict = asyncio.get_event_loop().run_until_complete(
            _send_and_collect(mock_send, MagicMock(), ctx)
        )
        assert needs_input is True
        assert response_dict is not None
