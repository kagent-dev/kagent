from __future__ import annotations

import logging
import re
from typing import AsyncGenerator, Optional

from google.adk.models.lite_llm import LiteLlm
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse

logger = logging.getLogger(__name__)

# Bedrock requires tool names to match [a-zA-Z0-9_-]+ with length >= 1
_BEDROCK_TOOL_NAME_RE = re.compile(r"^[a-zA-Z0-9_-]+$")
_BEDROCK_TOOL_NAME_FALLBACK = "unknown_tool"


def _is_bedrock_model(model: str) -> bool:
    return "bedrock" in model.lower()


def _sanitize_tool_name(name: str, idx: "int | str") -> str:
    """Return a Bedrock-safe tool name; replace invalid/empty names with a fallback."""
    if not name or not _BEDROCK_TOOL_NAME_RE.match(name):
        safe = re.sub(r"[^a-zA-Z0-9_-]", "_", name) if name else ""
        safe = safe or f"{_BEDROCK_TOOL_NAME_FALLBACK}_{idx}"
        logger.debug("Sanitized invalid Bedrock tool name %r -> %r", name, safe)
        return safe
    return name


def _sanitize_llm_request(llm_request: LlmRequest) -> None:
    """Fix tool names in the conversation history before sending to Bedrock."""
    for content in llm_request.contents:
        if not content.parts:
            continue
        for idx, part in enumerate(content.parts):
            fc = getattr(part, "function_call", None)
            if fc is not None and hasattr(fc, "name"):
                fc.name = _sanitize_tool_name(fc.name or "", idx)


def _sanitize_llm_response(response: LlmResponse, idx: int) -> LlmResponse:
    """Fix tool names in a model response before the ADK stores it in history."""
    if response.content and response.content.parts:
        for i, part in enumerate(response.content.parts):
            fc = getattr(part, "function_call", None)
            if fc is not None and hasattr(fc, "name"):
                # Use a composite suffix to avoid collisions across responses/parts.
                composite_suffix = f"{idx}_{i}"
                fc.name = _sanitize_tool_name(fc.name or "", composite_suffix)
    return response


class KAgentLiteLlm(LiteLlm):
    """LiteLlm subclass that supports API key passthrough and Bedrock tool name sanitization."""

    api_key_passthrough: Optional[bool] = None

    def __init__(self, model: str, **kwargs):
        passthrough = kwargs.pop("api_key_passthrough", None)
        super().__init__(model=model, **kwargs)
        self.api_key_passthrough = passthrough

    def set_passthrough_key(self, token: str) -> None:
        self._additional_args["api_key"] = token

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        effective_model = llm_request.model or self.model
        if _is_bedrock_model(effective_model):
            _sanitize_llm_request(llm_request)
            idx = 0
            async for response in super().generate_content_async(llm_request, stream=stream):
                yield _sanitize_llm_response(response, idx)
                idx += 1
        else:
            async for response in super().generate_content_async(llm_request, stream=stream):
                yield response
