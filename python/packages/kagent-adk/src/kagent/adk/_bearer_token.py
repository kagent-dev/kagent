"""Incoming request Bearer token, shared across consumers outside the
before_model_callback pipeline LLMPassthroughPlugin hooks into, and the
resolution order that turns it into the credential for an outbound LLM call
(see resolve_passthrough_token).

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
import logging
from typing import Optional, Protocol, runtime_checkable

logger = logging.getLogger(__name__)

bearer_token: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar("kagent_bearer_token", default=None)

session_id: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar("kagent_session_id", default=None)


@runtime_checkable
class ExchangedTokenProvider(Protocol):
    """Resolves the STS-exchanged token for one request, or None.

    The implementation owns mode handling, caller identity and expiry, so the
    LLM and MCP paths read the same lookup instead of the cache directly.
    """

    def exchanged_token(self, *, session_id: str, bearer_token: str) -> Optional[str]: ...


def extract_bearer_token(headers: dict) -> Optional[str]:
    """Extract the Bearer token from an A2A request's headers dict."""
    auth_header = headers.get("authorization") or headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return None
    token = auth_header[7:].strip()
    return token or None


def resolve_passthrough_token(
    provider: Optional[ExchangedTokenProvider],
    *,
    inbound_token: Optional[str],
    session_id: Optional[str],
) -> Optional[str]:
    """Token to authenticate an outbound LLM call with.

    The STS-exchanged token for the session wins over the caller's own bearer
    token: it names the user delegated to this agent, which is the identity the
    backend should see. Without STS configured no provider is passed and the
    caller's token is returned, as before.

    A request presenting no caller token gets None: the exchanged token replaces
    the caller's, it never stands in for its absence.

    Both request inputs are required, so an explicitly absent credential is
    distinguishable from an omitted argument. Callers outside the
    before_model_callback pipeline read the ContextVars and pass them in.
    """
    if not inbound_token:
        return None
    if provider is not None and session_id:
        try:
            exchanged = provider.exchanged_token(session_id=session_id, bearer_token=inbound_token)
        except Exception:
            logger.warning("Failed to read the exchanged token, falling back to the caller's token", exc_info=True)
            exchanged = None
        if exchanged:
            return exchanged
    return inbound_token
