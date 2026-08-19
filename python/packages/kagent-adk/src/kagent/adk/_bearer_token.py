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


def extract_bearer_token(headers: dict) -> Optional[str]:
    """Extract the Bearer token from an A2A request's headers dict."""
    auth_header = headers.get("authorization") or headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return None
    token = auth_header[7:].strip()
    return token or None
