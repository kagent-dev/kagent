"""Gemini model wrapper with kagent transport configuration."""

from __future__ import annotations

import os
from functools import cached_property
from typing import Optional

from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.utils._google_client_headers import get_tracking_headers
from google.genai import Client, types

from ._ssl import KAgentTLSMixin


def _merge_headers(extra_headers: Optional[dict[str, str]]) -> dict[str, str]:
    headers = get_tracking_headers()
    if extra_headers:
        headers.update(extra_headers)
    return headers


class _GeminiGenerationConfigMixin:
    """Applies model-level generation defaults (e.g. max_output_tokens) to each
    request, without overriding any per-request value the agent already set.

    The native Gemini/Vertex ADK model takes generation config from the
    per-request LlmRequest.config rather than the model definition, so this
    mixin bridges the ModelConfig-level setting onto the request.
    """

    async def generate_content_async(self, llm_request, stream: bool = False):
        max_output_tokens = getattr(self, "max_output_tokens", None)
        if max_output_tokens is not None:
            config = llm_request.config
            if config is None:
                config = types.GenerateContentConfig()
                llm_request.config = config
            if config.max_output_tokens is None:
                config.max_output_tokens = max_output_tokens
        async for response in super().generate_content_async(llm_request, stream=stream):
            yield response


class KAgentGeminiLlm(KAgentTLSMixin, _GeminiGenerationConfigMixin, GeminiLLM):
    """Gemini API model that applies kagent TLS and header settings."""

    extra_headers: Optional[dict[str, str]] = None
    api_key_passthrough: Optional[bool] = None
    max_output_tokens: Optional[int] = None

    model_config = {"arbitrary_types_allowed": True}

    def _http_options(self, *, api_version: str | None = None) -> types.HttpOptions:
        verify = self._tls_verify()
        kwargs = {}
        if verify is not None:
            kwargs = {
                "client_args": {"verify": verify},
                "async_client_args": {"verify": verify, "ssl": verify},
            }
        return types.HttpOptions(
            headers=_merge_headers(self.extra_headers),
            retry_options=self.retry_options,
            base_url=self.base_url,
            api_version=api_version,
            **kwargs,
        )

    @cached_property
    def api_client(self) -> Client:
        return Client(
            api_key=os.environ.get("GOOGLE_API_KEY") or os.environ.get("GEMINI_API_KEY"),
            http_options=self._http_options(),
        )

    @cached_property
    def _live_api_client(self) -> Client:
        return Client(
            api_key=os.environ.get("GOOGLE_API_KEY") or os.environ.get("GEMINI_API_KEY"),
            http_options=self._http_options(api_version=self._live_api_version),
        )


class KAgentGeminiVertexAILlm(_GeminiGenerationConfigMixin, GeminiLLM):
    """Gemini Vertex AI model.

    Auth (project/location/ADC) is handled by the native ADK client via the
    environment variables the controller sets, so the client is intentionally
    not overridden here. Only the model-level generation config is applied.
    """

    max_output_tokens: Optional[int] = None

    model_config = {"arbitrary_types_allowed": True}
