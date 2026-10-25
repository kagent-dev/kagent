"""Set an LLM API key from the current invocation's credential."""

import logging
from typing import Optional, Protocol, runtime_checkable

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.adk.plugins.base_plugin import BasePlugin

from ._invocation_auth import invocation_credential

logger = logging.getLogger(__name__)


@runtime_checkable
class SupportsPassthroughAuth(Protocol):
    """Protocol for models that support API key passthrough."""

    api_key_passthrough: Optional[bool]

    def set_passthrough_key(self, token: str) -> None: ...


class LLMPassthroughPlugin(BasePlugin):
    """Sets the LLM API key from the current invocation's credential."""

    def __init__(self):
        super().__init__(name="llm_passthrough")

    async def before_model_callback(
        self, *, callback_context: CallbackContext, llm_request: LlmRequest
    ) -> Optional[LlmResponse]:
        model = callback_context._invocation_context.agent.model
        if not isinstance(model, SupportsPassthroughAuth):
            return None
        if not model.api_key_passthrough:
            return None

        model.set_passthrough_key(await invocation_credential())
        logger.debug("Set LLM API key from invocation credential")
        return None
