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


class KAgentGeminiLlm(KAgentTLSMixin, GeminiLLM):
    """Gemini API model that applies kagent TLS and header settings."""

    extra_headers: Optional[dict[str, str]] = None
    api_key_passthrough: Optional[bool] = None

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
