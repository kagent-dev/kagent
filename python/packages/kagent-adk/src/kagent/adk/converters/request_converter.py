from typing import Any

from a2a.server.agent_execution import RequestContext
from google.adk.runners import RunConfig
from google.genai import types as genai_types

from kagent.core.a2a import extract_user_id

from .part_converter import convert_a2a_part_to_genai_part


def convert_a2a_request_to_adk_run_args(
    request: RequestContext,
) -> dict[str, Any]:
    """Convert an A2A request to ADK run arguments.

    Args:
        request: The A2A RequestContext to convert

    Returns:
        Dictionary with user_id, session_id, new_message, and run_config

    Raises:
        ValueError: If request.message is None
    """
    if not request.message:
        raise ValueError("Request message cannot be None")

    return {
        "user_id": extract_user_id(request),
        "session_id": request.context_id,
        "new_message": genai_types.Content(
            role="user",
            parts=[convert_a2a_part_to_genai_part(part) for part in request.message.parts],
        ),
        "run_config": RunConfig(),
    }
