"""Mistral AI model implementation.

Mistral exposes an OpenAI-compatible wire protocol (POST /v1/chat/completions
with a Bearer token), so KAgentMistralLlm subclasses BaseOpenAI to inherit
message conversion, tool schemas, streaming, and telemetry. Only the
discriminator, default base URL, and API-key environment variable differ.
"""

from __future__ import annotations

import os
from functools import cached_property
from typing import Literal, Optional

from openai import AsyncOpenAI

from ._openai import BaseOpenAI

DEFAULT_MISTRAL_BASE_URL = "https://api.mistral.ai/v1"


class KAgentMistralLlm(BaseOpenAI):
    """Mistral AI model (OpenAI-compatible endpoint)."""

    type: Literal["mistral"]

    @classmethod
    def supported_models(cls) -> list[str]:
        """Regex list for LlmRegistry. Covers current Mistral, Magistral, Codestral, Ministral, Pixtral, and Nemo names."""
        return [
            r"mistral-.*",
            r"magistral-.*",
            r"codestral-.*",
            r"ministral-.*",
            r"pixtral-.*",
            r"open-mistral-.*",
        ]

    @cached_property
    def _client(self) -> AsyncOpenAI:
        """OpenAI-compatible client pointed at Mistral's endpoint.

        API key resolution: explicit api_key (passthrough or config) wins,
        then MISTRAL_API_KEY. Base URL falls back to MISTRAL_API_BASE, then
        the Mistral cloud default.
        """
        api_key = self.api_key or os.environ.get("MISTRAL_API_KEY")
        if not api_key and not self.api_key_passthrough:
            raise ValueError(
                "Mistral API key must be provided via api_key parameter, "
                "MISTRAL_API_KEY environment variable, or api_key_passthrough."
            )

        base_url = self.base_url or os.environ.get("MISTRAL_API_BASE") or DEFAULT_MISTRAL_BASE_URL

        return AsyncOpenAI(
            api_key=api_key,
            base_url=base_url,
            default_headers=self.default_headers,
            timeout=self.timeout,
            http_client=self._create_http_client(),
        )
