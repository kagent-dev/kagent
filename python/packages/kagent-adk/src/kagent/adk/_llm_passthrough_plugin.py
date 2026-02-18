"""LLM API key passthrough plugin.

Sets the LLM API key from the incoming request's Bearer token.
Reuses the same session state headers pattern as create_header_provider()
and ADKTokenPropagationPlugin. Reads the Authorization header from
callback_context.state["headers"] (set by _agent_executor.py).
"""

import logging
from typing import Optional, Protocol, runtime_checkable

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.adk.plugins.base_plugin import BasePlugin

logger = logging.getLogger(__name__)


@runtime_checkable
class SupportsPassthroughAuth(Protocol):
    """Protocol for models that support API key passthrough."""

    api_key_passthrough: Optional[bool]

    def set_passthrough_key(self, token: str) -> None: ...


def _extract_bearer_token(callback_context: CallbackContext) -> Optional[str]:
    """Extract the Bearer token from session state headers."""
    headers = callback_context.state.get("headers", {})
    auth_header = headers.get("authorization") or headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return None
    token = auth_header[7:].strip()
    return token or None


class LLMPassthroughPlugin(BasePlugin):
    """Sets the LLM API key from the incoming request's Bearer token."""

    def __init__(self):
        super().__init__(name="llm_passthrough")

    async def before_model_callback(
        self, *, callback_context: CallbackContext, llm_request: LlmRequest
    ) -> Optional[LlmResponse]:
        token = _extract_bearer_token(callback_context)
        if not token:
            return None

        model = callback_context._invocation_context.agent.model
        if not isinstance(model, SupportsPassthroughAuth):
            return None
        if not model.api_key_passthrough:
            return None

        model.set_passthrough_key(token)
        logger.debug("Set LLM API key from Bearer token")
        return None
