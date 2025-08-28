from typing import Any

from a2a.server.agent_execution import RequestContext
from google.adk.runners import RunConfig
from google.genai import types as genai_types

from .part_converter import convert_a2a_part_to_genai_part


def _get_user_id(request: RequestContext) -> str:
    # Get user from call context if available (auth is enabled on a2a server)
    if request.call_context and request.call_context.user and request.call_context.user.user_name:
        return request.call_context.user.user_name

    # Get user from context id
    return f"A2A_USER_{request.context_id}"


def convert_a2a_request_to_adk_run_args(
    request: RequestContext,
) -> dict[str, Any]:
    if not request.message:
        raise ValueError("Request message cannot be None")

    return {
        "user_id": _get_user_id(request),
        "session_id": request.context_id,
        "new_message": genai_types.Content(
            role="user",
            parts=[convert_a2a_part_to_genai_part(part) for part in request.message.parts],
        ),
        "run_config": RunConfig(),
    }
