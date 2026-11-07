"""xAI model implementation.

xAI exposes an OpenAI-compatible wire protocol (POST /v1/chat/completions
with a Bearer token), so KAgentXAILlm subclasses BaseOpenAI to inherit
message conversion, tool schemas, streaming, and telemetry. Only the
discriminator, default base URL, and API-key environment variable differ.
"""

from __future__ import annotations

import os
from functools import cached_property
from typing import Literal

from openai import AsyncOpenAI

from ._openai import BaseOpenAI

DEFAULT_XAI_BASE_URL = "https://api.x.ai/v1"


class KAgentXAILlm(BaseOpenAI):
    """xAI Grok model (OpenAI-compatible endpoint)."""

    type: Literal["xai"]

    @classmethod
    def supported_models(cls) -> list[str]:
        """Regex list for LlmRegistry. Covers xAI Grok model names."""
        return [r"grok-.*"]

    @cached_property
    def _client(self) -> AsyncOpenAI:
        """OpenAI-compatible client pointed at xAI's endpoint.

        API key resolution: explicit api_key (passthrough or config) wins,
        then XAI_API_KEY. Base URL falls back to XAI_API_BASE, then the xAI
        cloud default.
        """
        api_key = self.api_key or os.environ.get("XAI_API_KEY")
        if not api_key and not self.api_key_passthrough:
            raise ValueError(
                "xAI API key must be provided via api_key parameter, "
                "XAI_API_KEY environment variable, or api_key_passthrough."
            )

        base_url = self.base_url or os.environ.get("XAI_API_BASE") or DEFAULT_XAI_BASE_URL

        return AsyncOpenAI(
            api_key=api_key,
            base_url=base_url,
            default_headers=self.default_headers,
            timeout=self.timeout,
            http_client=self._create_http_client(),
        )
