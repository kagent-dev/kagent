"""OpenTelemetry attribute helpers for kagent A2A (cross-agent) delegation.

A remote-agent (A2A) delegation is already wrapped by the ADK ``execute_tool
<subagent>`` span. Rather than add a new span layer (which would change the
trace structure), these helpers stamp kagent-specific delegation attributes
onto that active span so it reflects the actual cross-agent call: which
sub-agent was invoked, the conversation lineage (context ids used to keep the
sub-agent session continuous), and how the delegated task resolved.
"""

from typing import Optional

from opentelemetry import trace

# A2A delegation attribute keys.
ATTR_A2A_SUBAGENT_NAME = "a2a.subagent.name"
ATTR_A2A_CONTEXT_ID = "a2a.context_id"
ATTR_A2A_PARENT_CONTEXT_ID = "a2a.parent_context_id"
ATTR_A2A_ROOT_CONTEXT_ID = "a2a.root_context_id"
ATTR_A2A_TASK_ID = "a2a.task.id"
ATTR_A2A_TASK_STATE = "a2a.task.state"


def annotate_delegation_request(
    subagent_name: str,
    context_id: str,
    lineage_headers: Optional[dict] = None,
) -> None:
    """Stamp the outbound delegation shape onto the active span.

    Records which sub-agent is being called and the conversation lineage
    (parent/root context ids) that scopes the sub-agent session. No-op when no
    span is recording (tracing disabled).
    """
    span = trace.get_current_span()
    if not span.is_recording():
        return
    span.set_attribute(ATTR_A2A_SUBAGENT_NAME, subagent_name)
    if context_id:
        span.set_attribute(ATTR_A2A_CONTEXT_ID, context_id)
    if lineage_headers:
        parent = lineage_headers.get("x-kagent-parent-context-id")
        root = lineage_headers.get("x-kagent-root-context-id")
        if parent:
            span.set_attribute(ATTR_A2A_PARENT_CONTEXT_ID, parent)
        if root:
            span.set_attribute(ATTR_A2A_ROOT_CONTEXT_ID, root)


def annotate_delegation_result(task_id: Optional[str], task_state: Optional[str]) -> None:
    """Stamp the resolved task id/state of a delegation onto the active span."""
    span = trace.get_current_span()
    if not span.is_recording():
        return
    if task_id:
        span.set_attribute(ATTR_A2A_TASK_ID, task_id)
    if task_state:
        span.set_attribute(ATTR_A2A_TASK_STATE, task_state)
