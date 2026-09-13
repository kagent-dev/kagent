"""after_tool_callback that records the tool exchange on the execute_tool span.

ADK only writes tool arguments and responses under its own
``gcp.vertex.agent.tool_call_args`` / ``gcp.vertex.agent.tool_response`` keys.
Backends that follow the OTel GenAI semantic conventions read
``gen_ai.tool.call.arguments`` and ``gen_ai.tool.call.result`` instead, so the
span named after the tool rendered with an empty body
(kagent-dev/kagent#2734). ADK runs after-tool callbacks inside the
``execute_tool`` span, so the callback stamps the two attributes onto the
current span. Content capture follows the same ``TelemetryConfig`` switch ADK
uses for its legacy span payloads.
"""

from typing import Any

from google.adk.telemetry.context import TelemetryConfig
from google.adk.telemetry.tracing import safe_json_serialize
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext
from opentelemetry import trace

GEN_AI_TOOL_CALL_ARGUMENTS = "gen_ai.tool.call.arguments"
GEN_AI_TOOL_CALL_RESULT = "gen_ai.tool.call.result"


def _should_capture_content(tool_context: ToolContext) -> bool:
    """Mirrors ADK's telemetry config lookup: per-request RunConfig first, then env defaults."""
    invocation_context = getattr(tool_context, "_invocation_context", None)
    run_config = getattr(invocation_context, "run_config", None)
    config = getattr(run_config, "telemetry", None) or TelemetryConfig()
    return bool(config.should_add_content_to_legacy_spans)


def make_tool_span_attributes_callback():
    """Create an after_tool_callback that sets GenAI tool-call attributes on the execute_tool span.

    Returns:
        A callback compatible with Google ADK's after_tool_callback signature. It never
        alters the tool response.
    """

    def after_tool(
        tool: BaseTool,
        args: dict[str, Any],
        tool_context: ToolContext,
        tool_response: Any,
    ) -> None:
        span = trace.get_current_span()
        if not span.is_recording():
            return None
        if _should_capture_content(tool_context):
            response = tool_response if isinstance(tool_response, dict) else {"result": tool_response}
            span.set_attribute(GEN_AI_TOOL_CALL_ARGUMENTS, safe_json_serialize(args))
            span.set_attribute(GEN_AI_TOOL_CALL_RESULT, safe_json_serialize(response))
        else:
            span.set_attribute(GEN_AI_TOOL_CALL_ARGUMENTS, "{}")
            span.set_attribute(GEN_AI_TOOL_CALL_RESULT, "{}")
        return None

    return after_tool
