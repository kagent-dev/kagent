"""Gemini model wrapper with kagent transport configuration."""

from __future__ import annotations

import os
from functools import cached_property
from typing import Optional
from urllib.parse import quote

import httpx
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.utils._google_client_headers import get_tracking_headers
from google.genai import Client, types

from ._ssl import KAgentTLSMixin

_CREDENTIAL_PLACEHOLDER = "kagent-gemini-placeholder"


def _remove_placeholder(request: httpx.Request) -> None:
    if request.headers.get("x-goog-api-key") == _CREDENTIAL_PLACEHOLDER:
        del request.headers["x-goog-api-key"]


async def _remove_placeholder_async(request: httpx.Request) -> None:
    _remove_placeholder(request)


class _ProxyAwareAsyncTransport(httpx.AsyncBaseTransport):
    """Select HTTPX while retaining its environment proxy and TLS handling."""

    def __init__(self, verify):
        self._client = httpx.AsyncClient(verify=verify)

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        return await self._client.send(request, stream=True)

    async def aclose(self) -> None:
        await self._client.aclose()


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

    async def generate_content_async(self, llm_request, stream: bool = False):
        if self.base_url:
            # Custom endpoints treat the model ID as one URL path segment,
            # not a resource path. Encode only the SDK request's copy so the
            # configured ID and the caller's request remain unchanged.
            llm_request = llm_request.model_copy(update={"model": quote(llm_request.model or self.model, safe="")})
        async for response in super().generate_content_async(llm_request, stream=stream):
            yield response

    def _http_options(self, *, api_version: str | None = None) -> types.HttpOptions:
        verify = self._tls_verify()
        kwargs = {}
        if verify is not None:
            kwargs = {
                "client_args": {"verify": verify},
                "async_client_args": {"verify": verify, "ssl": verify},
            }
        if self.base_url or self._has_authorization_header():
            # Force httpx for async requests too, so aiohttp cannot bypass the
            # hook removing the SDK-only key. The SDK owns and closes clients.
            kwargs = {
                "client_args": {
                    "verify": verify if verify is not None else True,
                    "event_hooks": {"request": [_remove_placeholder]},
                },
                "async_client_args": {
                    "transport": _ProxyAwareAsyncTransport(verify=verify if verify is not None else True),
                    "event_hooks": {"request": [_remove_placeholder_async]},
                },
            }
        if self.base_url:
            api_version = ""
        return types.HttpOptions(
            headers=_merge_headers(self.extra_headers),
            retry_options=self.retry_options,
            base_url=self.base_url,
            api_version=api_version,
            **kwargs,
        )

    def _api_key(self) -> str | None:
        api_key = os.environ.get("GOOGLE_API_KEY") or os.environ.get("GEMINI_API_KEY")
        if self._has_authorization_header() or (self.base_url and not api_key):
            api_key = _CREDENTIAL_PLACEHOLDER
        return api_key

    @cached_property
    def api_client(self) -> Client:
        return Client(
            vertexai=False,
            api_key=self._api_key(),
            http_options=self._http_options(),
        )

    def _has_authorization_header(self) -> bool:
        return any(header.lower() == "authorization" for header in self.extra_headers or {})

    @cached_property
    def _live_api_client(self) -> Client:
        return Client(
            vertexai=False,
            api_key=self._api_key(),
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
