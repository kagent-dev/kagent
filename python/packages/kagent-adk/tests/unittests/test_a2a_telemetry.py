"""Tests for kagent A2A delegation span annotations."""

from opentelemetry.sdk.trace import TracerProvider

from kagent.adk import _a2a_telemetry as a2atel


def _tracer():
    return TracerProvider().get_tracer("test")


def test_annotate_delegation_request_stamps_subagent_and_lineage():
    tracer = _tracer()
    with tracer.start_as_current_span("execute_tool bmad_architect") as span:
        a2atel.annotate_delegation_request(
            "bmad_architect",
            "ctx-123",
            {
                "x-kagent-parent-context-id": "parent-1",
                "x-kagent-root-context-id": "root-0",
            },
        )
    attrs = span.attributes
    assert attrs[a2atel.ATTR_A2A_SUBAGENT_NAME] == "bmad_architect"
    assert attrs[a2atel.ATTR_A2A_CONTEXT_ID] == "ctx-123"
    assert attrs[a2atel.ATTR_A2A_PARENT_CONTEXT_ID] == "parent-1"
    assert attrs[a2atel.ATTR_A2A_ROOT_CONTEXT_ID] == "root-0"


def test_annotate_delegation_result_stamps_task_id_and_state():
    tracer = _tracer()
    with tracer.start_as_current_span("execute_tool bmad_architect") as span:
        a2atel.annotate_delegation_result("task-9", "completed")
    attrs = span.attributes
    assert attrs[a2atel.ATTR_A2A_TASK_ID] == "task-9"
    assert attrs[a2atel.ATTR_A2A_TASK_STATE] == "completed"


def test_annotate_is_noop_without_recording_span():
    # No active recording span -> must not raise.
    a2atel.annotate_delegation_request("x", "ctx", None)
    a2atel.annotate_delegation_result("t", "completed")
