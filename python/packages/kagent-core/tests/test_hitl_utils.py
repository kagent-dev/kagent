"""Tests for HITL utility functions in kagent.core.a2a._hitl."""

from a2a.types import DataPart, Message, Part, Role, Task, TaskState, TaskStatus

from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_DENY,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_REJECTION_REASONS_KEY,
    HitlPartInfo,
    OriginalFunctionCall,
    extract_ask_user_answers_from_message,
    extract_batch_decisions_from_message,
    extract_decision_from_message,
    extract_hitl_info_from_task,
    extract_rejection_reasons_from_message,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_message(*data_parts: dict) -> Message:
    """Build a Message with one or more DataPart dicts."""
    return Message(
        role=Role.user,
        message_id="test",
        task_id="task1",
        context_id="ctx1",
        parts=[Part(DataPart(data=d)) for d in data_parts],
    )


def _make_hitl_task(*hitl_data_dicts: dict) -> Task:
    """Build a Task whose status message contains HITL DataParts."""
    parts = []
    for d in hitl_data_dicts:
        parts.append(
            Part(
                DataPart(
                    data=d,
                    metadata={
                        "adk_type": "function_call",
                        "adk_is_long_running": True,
                    },
                )
            )
        )
    return Task(
        id="task-1",
        context_id="ctx-1",
        status=TaskStatus(
            state=TaskState.input_required,
            message=Message(
                role=Role.agent,
                message_id="msg-1",
                parts=parts,
            ),
        ),
    )


# ===================================================================
# extract_decision_from_message
# ===================================================================


class TestExtractDecisionFromMessage:
    def test_approve(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE})
        assert extract_decision_from_message(msg) == KAGENT_HITL_DECISION_TYPE_APPROVE

    def test_deny(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY})
        assert extract_decision_from_message(msg) == KAGENT_HITL_DECISION_TYPE_DENY

    def test_batch(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                "decisions": {"t1": "approve"},
            }
        )
        assert extract_decision_from_message(msg) == KAGENT_HITL_DECISION_TYPE_BATCH

    def test_reject_alias(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: "reject"})
        assert extract_decision_from_message(msg) == "reject"

    def test_none_message(self):
        assert extract_decision_from_message(None) is None

    def test_empty_parts(self):
        msg = Message(role=Role.user, message_id="x", task_id="t", context_id="c", parts=[])
        assert extract_decision_from_message(msg) is None

    def test_no_decision_key(self):
        msg = _make_message({"some_other_key": "value"})
        assert extract_decision_from_message(msg) is None

    def test_invalid_decision_value(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: "unknown_value"})
        assert extract_decision_from_message(msg) is None


# ===================================================================
# extract_batch_decisions_from_message
# ===================================================================


class TestExtractBatchDecisionsFromMessage:
    def test_basic(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                "decisions": {"tool1": "approve", "tool2": "deny"},
            }
        )
        assert extract_batch_decisions_from_message(msg) == {
            "tool1": "approve",
            "tool2": "deny",
        }

    def test_none_message(self):
        assert extract_batch_decisions_from_message(None) is None

    def test_non_batch_decision_type(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                "decisions": {"tool1": "approve"},
            }
        )
        assert extract_batch_decisions_from_message(msg) is None

    def test_missing_decisions_key(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH})
        assert extract_batch_decisions_from_message(msg) is None

    def test_invalid_decision_values_filtered(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                "decisions": {
                    "tool1": "approve",
                    "tool2": "invalid_value",
                    "tool3": "deny",
                },
            }
        )
        result = extract_batch_decisions_from_message(msg)
        assert result == {"tool1": "approve", "tool3": "deny"}

    def test_all_invalid_returns_none(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                "decisions": {"tool1": "bogus"},
            }
        )
        assert extract_batch_decisions_from_message(msg) is None


# ===================================================================
# extract_rejection_reasons_from_message
# ===================================================================


class TestExtractRejectionReasonsFromMessage:
    def test_uniform_deny_with_reason(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY,
                "rejection_reason": "Too dangerous",
            }
        )
        assert extract_rejection_reasons_from_message(msg) == {"*": "Too dangerous"}

    def test_uniform_deny_without_reason(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY})
        assert extract_rejection_reasons_from_message(msg) is None

    def test_uniform_deny_empty_reason(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_DENY,
                "rejection_reason": "",
            }
        )
        assert extract_rejection_reasons_from_message(msg) is None

    def test_batch_with_reasons(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                KAGENT_HITL_REJECTION_REASONS_KEY: {
                    "tool1": "Not needed",
                    "tool2": "Risky",
                },
            }
        )
        assert extract_rejection_reasons_from_message(msg) == {
            "tool1": "Not needed",
            "tool2": "Risky",
        }

    def test_batch_without_reasons(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
            }
        )
        assert extract_rejection_reasons_from_message(msg) is None

    def test_batch_empty_reasons_filtered(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                KAGENT_HITL_REJECTION_REASONS_KEY: {"tool1": ""},
            }
        )
        assert extract_rejection_reasons_from_message(msg) is None

    def test_approve_has_no_reasons(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                "rejection_reason": "This should be ignored",
            }
        )
        assert extract_rejection_reasons_from_message(msg) is None

    def test_none_message(self):
        assert extract_rejection_reasons_from_message(None) is None

    def test_reject_alias_with_reason(self):
        """The 'reject' alias should also extract reasons."""
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: "reject",
                "rejection_reason": "Alias rejection",
            }
        )
        assert extract_rejection_reasons_from_message(msg) == {"*": "Alias rejection"}


# ===================================================================
# extract_ask_user_answers_from_message
# ===================================================================


class TestExtractAskUserAnswersFromMessage:
    def test_basic(self):
        answers = [{"answer": ["PostgreSQL"]}, {"answer": ["Auth", "Caching"]}]
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                "ask_user_answers": answers,
            }
        )
        assert extract_ask_user_answers_from_message(msg) == answers

    def test_none_message(self):
        assert extract_ask_user_answers_from_message(None) is None

    def test_no_answers_key(self):
        msg = _make_message({KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE})
        assert extract_ask_user_answers_from_message(msg) is None

    def test_answers_not_a_list(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                "ask_user_answers": "not-a-list",
            }
        )
        assert extract_ask_user_answers_from_message(msg) is None

    def test_empty_answers_list(self):
        msg = _make_message(
            {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                "ask_user_answers": [],
            }
        )
        # Empty list is falsy but still a list — function returns it
        result = extract_ask_user_answers_from_message(msg)
        assert result == []


# ===================================================================
# OriginalFunctionCall model
# ===================================================================


class TestOriginalFunctionCall:
    def test_basic_construction(self):
        fc = OriginalFunctionCall(name="delete_file", args={"path": "/tmp/x"}, id="call_123")
        assert fc.name == "delete_file"
        assert fc.args == {"path": "/tmp/x"}
        assert fc.id == "call_123"

    def test_defaults(self):
        fc = OriginalFunctionCall(name="read_file")
        assert fc.args == {}
        assert fc.id is None

    def test_model_dump(self):
        fc = OriginalFunctionCall(name="delete_file", args={"path": "/tmp/x"}, id="call_123")
        dumped = fc.model_dump()
        assert dumped == {"name": "delete_file", "args": {"path": "/tmp/x"}, "id": "call_123"}


# ===================================================================
# HitlPartInfo model
# ===================================================================


class TestHitlPartInfo:
    def test_construction_with_alias(self):
        """Test construction using camelCase alias (as from JSON wire format)."""
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            id="conf_1",
            originalFunctionCall=OriginalFunctionCall(name="delete_file", args={"path": "/tmp/x"}, id="call_123"),
        )
        assert hp.name == "adk_request_confirmation"
        assert hp.id == "conf_1"
        assert hp.original_function_call.name == "delete_file"

    def test_construction_with_python_name(self):
        """Test construction using snake_case Python field name."""
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            original_function_call=OriginalFunctionCall(name="read_file"),
        )
        assert hp.original_function_call.name == "read_file"

    def test_from_data_part_data(self):
        data = {
            "name": "adk_request_confirmation",
            "id": "conf_1",
            "args": {
                "originalFunctionCall": {
                    "name": "delete_file",
                    "args": {"path": "/tmp/x"},
                    "id": "call_123",
                },
                "toolConfirmation": {"hint": "Approve?", "confirmed": False},
            },
        }
        hp = HitlPartInfo.from_data_part_data(data)
        assert hp.name == "adk_request_confirmation"
        assert hp.id == "conf_1"
        assert hp.original_function_call.name == "delete_file"
        assert hp.original_function_call.args == {"path": "/tmp/x"}
        assert hp.original_function_call.id == "call_123"

    def test_from_data_part_data_minimal(self):
        """Minimal dict — only required fields."""
        data = {"args": {"originalFunctionCall": {"name": "list_files"}}}
        hp = HitlPartInfo.from_data_part_data(data)
        assert hp.name == "adk_request_confirmation"
        assert hp.id is None
        assert hp.original_function_call.name == "list_files"

    def test_tool_name_property(self):
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            originalFunctionCall=OriginalFunctionCall(name="write_file"),
        )
        assert hp.tool_name == "write_file"

    def test_tool_call_id_property(self):
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            originalFunctionCall=OriginalFunctionCall(name="write_file", id="call_abc"),
        )
        assert hp.tool_call_id == "call_abc"

    def test_tool_call_id_none(self):
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            originalFunctionCall=OriginalFunctionCall(name="write_file"),
        )
        assert hp.tool_call_id is None

    def test_model_dump_by_alias_produces_camel_case(self):
        """model_dump(by_alias=True) must produce camelCase for wire format."""
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            id="conf_1",
            originalFunctionCall=OriginalFunctionCall(name="delete_file", args={"path": "/tmp/x"}, id="call_123"),
        )
        dumped = hp.model_dump(by_alias=True)
        assert "originalFunctionCall" in dumped
        assert "original_function_call" not in dumped
        assert dumped["originalFunctionCall"]["name"] == "delete_file"

    def test_roundtrip_json(self):
        """Ensure model_dump_json → model_validate_json roundtrip preserves data."""
        hp = HitlPartInfo(
            name="adk_request_confirmation",
            id="conf_1",
            originalFunctionCall=OriginalFunctionCall(name="delete_file", args={"path": "/tmp/x"}, id="call_123"),
        )
        json_str = hp.model_dump_json(by_alias=True)
        restored = HitlPartInfo.model_validate_json(json_str)
        assert restored.name == hp.name
        assert restored.id == hp.id
        assert restored.original_function_call.name == hp.original_function_call.name
        assert restored.original_function_call.args == hp.original_function_call.args
        assert restored.original_function_call.id == hp.original_function_call.id


# ===================================================================
# extract_hitl_info_from_task
# ===================================================================


class TestExtractHitlInfoFromTask:
    def test_single_hitl_part(self):
        task = _make_hitl_task(
            {
                "name": "adk_request_confirmation",
                "id": "conf_1",
                "args": {
                    "originalFunctionCall": {
                        "name": "delete_file",
                        "args": {"path": "/tmp/x"},
                        "id": "call_1",
                    },
                },
            }
        )
        result = extract_hitl_info_from_task(task)
        assert result is not None
        assert len(result) == 1
        assert result[0].tool_name == "delete_file"
        assert result[0].original_function_call.id == "call_1"

    def test_multiple_hitl_parts(self):
        task = _make_hitl_task(
            {
                "name": "adk_request_confirmation",
                "id": "conf_1",
                "args": {
                    "originalFunctionCall": {"name": "delete_file", "args": {"path": "/a"}, "id": "c1"},
                },
            },
            {
                "name": "adk_request_confirmation",
                "id": "conf_2",
                "args": {
                    "originalFunctionCall": {"name": "write_file", "args": {"path": "/b"}, "id": "c2"},
                },
            },
        )
        result = extract_hitl_info_from_task(task)
        assert result is not None
        assert len(result) == 2
        assert result[0].tool_name == "delete_file"
        assert result[1].tool_name == "write_file"

    def test_no_message(self):
        task = Task(id="t", context_id="c", status=TaskStatus(state=TaskState.completed))
        assert extract_hitl_info_from_task(task) is None

    def test_no_parts(self):
        task = Task(
            id="t",
            context_id="c",
            status=TaskStatus(
                state=TaskState.input_required,
                message=Message(role=Role.agent, message_id="m", parts=[]),
            ),
        )
        assert extract_hitl_info_from_task(task) is None

    def test_non_hitl_data_parts_skipped(self):
        """DataParts without is_long_running metadata should be ignored."""
        task = Task(
            id="t",
            context_id="c",
            status=TaskStatus(
                state=TaskState.input_required,
                message=Message(
                    role=Role.agent,
                    message_id="m",
                    parts=[
                        Part(
                            DataPart(
                                data={"name": "some_function", "args": {}},
                                metadata={"adk_type": "function_call"},
                            )
                        ),
                    ],
                ),
            ),
        )
        assert extract_hitl_info_from_task(task) is None

    def test_kagent_prefix_metadata(self):
        """Should work with kagent_ prefix as well as adk_ prefix."""
        task = Task(
            id="t",
            context_id="c",
            status=TaskStatus(
                state=TaskState.input_required,
                message=Message(
                    role=Role.agent,
                    message_id="m",
                    parts=[
                        Part(
                            DataPart(
                                data={
                                    "name": "adk_request_confirmation",
                                    "id": "conf_1",
                                    "args": {
                                        "originalFunctionCall": {"name": "delete_file", "args": {}, "id": "c1"},
                                    },
                                },
                                metadata={
                                    "kagent_type": "function_call",
                                    "kagent_is_long_running": True,
                                },
                            )
                        ),
                    ],
                ),
            ),
        )
        result = extract_hitl_info_from_task(task)
        assert result is not None
        assert len(result) == 1
        assert result[0].tool_name == "delete_file"
