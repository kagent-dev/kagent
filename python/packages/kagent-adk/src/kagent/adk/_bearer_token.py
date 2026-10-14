"""Incoming request Bearer token, shared across consumers outside the
before_model_callback pipeline LLMPassthroughPlugin hooks into.

Memory (KagentMemoryService/KAgentEmbedding) is the motivating case: its
embedding calls are triggered from add_session_to_memory (an
asyncio.create_task background task with no callback_context of its own),
SaveMemoryTool, and PrefetchMemoryTool - none of which run as a
before_model_callback. A ContextVar avoids threading a token parameter
through google.adk's own BaseMemoryService interface (add_session_to_memory's
signature is fixed by ADK's runner, not by kagent-adk). asyncio.create_task
copies the current contextvars.Context at task-creation time, so a background
task still sees the token of the request that scheduled it.
"""

import contextvars
from typing import Optional

bearer_token: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar("kagent_bearer_token", default=None)


# A caller that authenticates with its own bearer token sends the model API key
# here instead, and passthrough picks it over that credential. The value is the
# key itself, with no scheme.
MODEL_API_KEY_HEADER = "x-kagent-model-api-key"


def extract_passthrough_token(headers: dict) -> Optional[str]:
    """Return the model API key for passthrough from an A2A request's headers dict.

    The model API key header wins; otherwise the Authorization Bearer token,
    for callers whose identity token is also their model credential.
    """
    model_api_key = (headers.get(MODEL_API_KEY_HEADER) or "").strip()
    if model_api_key:
        return model_api_key
    auth_header = headers.get("authorization") or headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return None
    token = auth_header[7:].strip()
    return token or None
