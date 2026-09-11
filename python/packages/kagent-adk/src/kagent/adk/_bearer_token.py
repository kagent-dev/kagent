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
    """Returns the STS-exchanged token cached for a session, or None."""

    def get_token_for_session(self, session_id: str) -> Optional[str]: ...


_exchanged_token_provider: Optional[ExchangedTokenProvider] = None


def set_exchanged_token_provider(provider: Optional[ExchangedTokenProvider]) -> None:
    """Register the STS plugin as the source of exchanged tokens. Called once at startup."""
    global _exchanged_token_provider
    _exchanged_token_provider = provider


def extract_bearer_token(headers: dict) -> Optional[str]:
    """Extract the Bearer token from an A2A request's headers dict."""
    auth_header = headers.get("authorization") or headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return None
    token = auth_header[7:].strip()
    return token or None


def resolve_passthrough_token(
    inbound_token: Optional[str] = None, current_session_id: Optional[str] = None
) -> Optional[str]:
    """Token to authenticate an outbound LLM call with.

    The STS-exchanged token for the session wins over the caller's own bearer
    token: it names the user delegated to this agent, which is the identity the
    backend should see. Without STS configured no provider is registered and the
    caller's token is returned, as before.
    """
    exchanged = _exchanged_token(current_session_id if current_session_id is not None else session_id.get())
    if exchanged:
        return exchanged
    return inbound_token if inbound_token is not None else bearer_token.get()


def _exchanged_token(current_session_id: Optional[str]) -> Optional[str]:
    if _exchanged_token_provider is None or not current_session_id:
        return None
    try:
        return _exchanged_token_provider.get_token_for_session(current_session_id)
    except Exception:
        logger.warning("Failed to read the exchanged token, falling back to the caller's token", exc_info=True)
        return None
