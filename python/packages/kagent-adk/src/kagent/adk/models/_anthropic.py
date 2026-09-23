"""Anthropic model implementation with api_key_passthrough, base_url, and header support."""

from __future__ import annotations

import logging
import os
from functools import cached_property
from typing import Optional

from anthropic import AsyncAnthropic
from google.adk.models.anthropic_llm import AnthropicLlm

from ._azure import (
    build_foundry_anthropic_client,
    resolve_azure_api_key,
    resolve_foundry_endpoint_deployment,
)
from ._ssl import KAgentTLSMixin

logger = logging.getLogger(__name__)


class KAgentAnthropicLlm(KAgentTLSMixin, AnthropicLlm):
    """Anthropic model with api_key_passthrough, custom base_url, header, and TLS support."""

    api_key_passthrough: Optional[bool] = None

    _api_key: Optional[str] = None
    base_url: Optional[str] = None
    extra_headers: Optional[dict[str, str]] = None

    model_config = {"arbitrary_types_allowed": True}

    def set_passthrough_key(self, token: str) -> None:
        """Forward the Bearer token from the incoming A2A request as the Anthropic API key."""
        if self._api_key != token:
            self._api_key = token
            # The SDK client captures auth at construction, so rebuild it only when the token changes.
            self.__dict__.pop("_anthropic_client", None)

    def _create_http_client(self):
        """Create HTTP client with custom SSL context using Anthropic SDK defaults.

        Returns:
            httpx.AsyncClient with SSL configuration, or None if no TLS config
        """
        return self._httpx_async_client_if_tls()

    @cached_property
    def _anthropic_client(self) -> AsyncAnthropic:
        api_key = self._api_key or os.environ.get("ANTHROPIC_API_KEY")
        kwargs = {}
        if api_key:
            kwargs["api_key"] = api_key
        if self.base_url:
            kwargs["base_url"] = self.base_url
        if self.extra_headers:
            kwargs["default_headers"] = self.extra_headers

        # Use the httpx.AsyncClient with SSL configuration if present
        http_client = self._create_http_client()
        if http_client is not None:
            kwargs["http_client"] = http_client

        return AsyncAnthropic(**kwargs)


class FoundryAnthropic(KAgentAnthropicLlm):
    """Claude on Azure AI Foundry's Anthropic Messages API."""

    endpoint: Optional[str] = None
    deployment: Optional[str] = None

    def _resolve_model_name(self, model: Optional[str]) -> str:
        del model
        _, deployment = resolve_foundry_endpoint_deployment(self.endpoint, self.deployment)
        return deployment

    @cached_property
    def _anthropic_client(self) -> AsyncAnthropic:
        endpoint, _ = resolve_foundry_endpoint_deployment(self.endpoint, self.deployment)
        api_key = resolve_azure_api_key(
            self._api_key,
            api_key_passthrough=self.api_key_passthrough,
            environment_variable="FOUNDRY_API_KEY",
        )
        return build_foundry_anthropic_client(
            endpoint=endpoint,
            api_key=api_key,
            api_key_passthrough=self.api_key_passthrough,
            default_headers=self.extra_headers,
            http_client=self._create_http_client(),
        )
