"""AWS Bedrock model implementation using the Anthropic SDK's Bedrock client."""

from __future__ import annotations

from functools import cached_property
from typing import Optional

from anthropic import AsyncAnthropicBedrock
from google.adk.models.anthropic_llm import AnthropicLlm


class KAgentBedrockLlm(AnthropicLlm):
    """Anthropic Claude models served via AWS Bedrock.

    Uses the official Anthropic SDK's ``AsyncAnthropicBedrock`` client, which
    authenticates using standard AWS credential chain (env vars, IAM role, etc.).
    """

    extra_headers: Optional[dict[str, str]] = None

    model_config = {"arbitrary_types_allowed": True}

    @cached_property
    def _anthropic_client(self) -> AsyncAnthropicBedrock:
        kwargs = {}
        if self.extra_headers:
            kwargs["default_headers"] = self.extra_headers
        return AsyncAnthropicBedrock(**kwargs)
