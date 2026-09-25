"""Anthropic model implementation with api_key_passthrough, base_url, header, and prompt caching support."""

from __future__ import annotations

import logging
import os
from collections.abc import AsyncGenerator, AsyncIterator
from contextvars import ContextVar
from functools import cached_property
from typing import Any, Literal, Optional

from anthropic import AsyncAnthropic
from anthropic.resources.messages import AsyncMessages
from anthropic.types import CacheControlEphemeralParam, Usage
from google.adk.models.anthropic_llm import AnthropicLlm
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types

from ._ssl import KAgentTLSMixin

logger = logging.getLogger(__name__)

# google-adk 1.x maps only input_tokens/output_tokens for Anthropic and drops the
# prompt-cache breakdown at the adapter boundary. The request proxy records the
# raw usage of the request it shaped, per task, so generate_content_async can
# fold it into the response google-adk builds. A ContextVar keeps concurrent
# requests on the same model apart.
_observed_usage: ContextVar[Optional[Usage]] = ContextVar("kagent_anthropic_usage", default=None)

# Anthropic rejects a cache breakpoint on a reasoning block, so the conversation
# breakpoint skips past them when the latest turn ends in one.
_UNCACHEABLE_BLOCK_TYPES = frozenset({"thinking", "redacted_thinking"})


def cache_control_param(cache_ttl: Optional[str] = None) -> CacheControlEphemeralParam:
    """Return the ``cache_control`` marker for the configured retention window.

    ``None`` or ``"5m"`` omits ``ttl``, giving the API's default 5-minute cache;
    ``"1h"`` opts into the 1-hour cache, whose writes are billed at a higher rate
    (see the ModelConfig CRD's ``anthropic.cacheTTL`` doc for the trade-off).
    """
    if cache_ttl == "1h":
        return {"type": "ephemeral", "ttl": "1h"}
    return {"type": "ephemeral"}


def mark_prompt_cache_breakpoints(kwargs: dict[str, Any], cache_control: CacheControlEphemeralParam) -> None:
    """Attach the prompt-cache breakpoints to a ``messages.create`` request, in place.

    The Messages API renders tools, then system, then messages, and caches the
    prefix up to each breakpoint. Marking the last tool definition, the last
    system block and the last block of the latest turn keeps the stable head of
    an agent loop cached while the conversation grows: every call reads the
    previous prefix from the cache and writes only the new turn. That uses three
    of the four breakpoints Anthropic allows per request.

    Marked blocks are copied rather than mutated so the caller's own message and
    tool objects stay untouched.
    """
    tools = kwargs.get("tools")
    if isinstance(tools, list) and tools:
        kwargs["tools"] = [*tools[:-1], {**tools[-1], "cache_control": cache_control}]

    system = kwargs.get("system")
    if isinstance(system, str) and system:
        kwargs["system"] = [{"type": "text", "text": system, "cache_control": cache_control}]
    elif isinstance(system, list) and system:
        kwargs["system"] = [*system[:-1], {**system[-1], "cache_control": cache_control}]

    messages = kwargs.get("messages")
    if not isinstance(messages, list):
        return
    for index in range(len(messages) - 1, -1, -1):
        message = messages[index]
        if not isinstance(message, dict):
            continue
        content = message.get("content")
        if isinstance(content, str):
            content = [{"type": "text", "text": content}] if content else []
        if not isinstance(content, list):
            continue
        for block_index in range(len(content) - 1, -1, -1):
            block = content[block_index]
            if not isinstance(block, dict) or block.get("type") in _UNCACHEABLE_BLOCK_TYPES:
                continue
            marked = [*content]
            marked[block_index] = {**block, "cache_control": cache_control}
            kwargs["messages"] = [*messages[:index], {**message, "content": marked}, *messages[index + 1 :]]
            return


def fold_cache_usage(
    usage_metadata: types.GenerateContentResponseUsageMetadata, usage: Usage
) -> types.GenerateContentResponseUsageMetadata:
    """Fold Anthropic's cache usage into the GenAI usage shape.

    Anthropic reports tokens served from the prompt cache and tokens written to
    it in their own fields, disjoint from ``input_tokens``, whereas GenAI expects
    one prompt count with the cached portion as a breakdown of it (this is also
    how google-adk 2.x maps Anthropic usage). Folding them in keeps the prompt
    count equal to the prompt the model actually saw whether or not caching is
    on, and ``cached_content_token_count`` says how much of it was a cache read.
    """
    cache_read = usage.cache_read_input_tokens or 0
    prompt = (usage.input_tokens or 0) + cache_read + (usage.cache_creation_input_tokens or 0)
    return usage_metadata.model_copy(
        update={
            "prompt_token_count": prompt,
            "cached_content_token_count": cache_read,
            "total_token_count": prompt + (usage_metadata.candidates_token_count or 0),
        }
    )


async def _observe_stream_usage(stream: AsyncIterator[Any]) -> AsyncIterator[Any]:
    """Pass the raw stream events through, recording the usage from ``message_start``."""
    async for event in stream:
        if getattr(event, "type", None) == "message_start":
            _observed_usage.set(event.message.usage)
        yield event


class PromptCachingMessages:
    """``AsyncMessages`` stand-in that marks the reusable prompt prefix before each request.

    google-adk's ``AnthropicLlm`` builds every request itself and hands it to
    ``client.messages.create`` on both the streaming and the non-streaming path,
    so the client is the one seam where kagent can shape the request without
    re-implementing the request builder. Everything but ``create`` is delegated
    to the wrapped resource. ``create`` also records the response usage (see
    ``_observed_usage``) because google-adk 1.x does not surface the cache
    breakdown itself.
    """

    def __init__(self, messages: AsyncMessages, cache_control: CacheControlEphemeralParam) -> None:
        self._messages = messages
        self._cache_control = cache_control

    def __getattr__(self, name: str) -> Any:
        return getattr(self._messages, name)

    async def create(self, **kwargs: Any) -> Any:
        mark_prompt_cache_breakpoints(kwargs, self._cache_control)
        result = await self._messages.create(**kwargs)
        if kwargs.get("stream"):
            return _observe_stream_usage(result)
        _observed_usage.set(getattr(result, "usage", None))
        return result


class KAgentAnthropicLlm(KAgentTLSMixin, AnthropicLlm):
    """Anthropic model with api_key_passthrough, custom base_url, header, TLS, and prompt caching support."""

    api_key_passthrough: Optional[bool] = None

    _api_key: Optional[str] = None
    base_url: Optional[str] = None
    extra_headers: Optional[dict[str, str]] = None
    # When True, every request carries cache_control breakpoints on the last
    # tool definition, the last system block and the last block of the latest
    # turn, so Anthropic bills the stable prefix of the agent loop as a cache
    # read. See mark_prompt_cache_breakpoints.
    prompt_caching: bool = False
    # When prompt_caching is on, cache_ttl selects the retention window: "5m"/None
    # (the API's default 5-minute cache) or "1h" (higher cache-write cost). See
    # cache_control_param.
    cache_ttl: Optional[Literal["5m", "1h"]] = None

    model_config = {"arbitrary_types_allowed": True}

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        responses = super().generate_content_async(llm_request, stream)
        if not self.prompt_caching:
            async for response in responses:
                yield response
            return
        # The proxy records the raw usage while google-adk consumes the request;
        # fold it into whatever usage google-adk attached to each response.
        token = _observed_usage.set(None)
        try:
            async for response in responses:
                usage = _observed_usage.get()
                if response.usage_metadata is not None and usage is not None:
                    response.usage_metadata = fold_cache_usage(response.usage_metadata, usage)
                yield response
        finally:
            _observed_usage.reset(token)

    def set_passthrough_key(self, token: str) -> None:
        """Forward the Bearer token from the incoming A2A request as the Anthropic API key."""
        self._api_key = token
        # Invalidate cached clients so they're recreated with the new key
        self.__dict__.pop("_anthropic_client", None)
        self.__dict__.pop("_http_client", None)

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

        client = AsyncAnthropic(**kwargs)
        if self.prompt_caching:
            # `messages` is a functools.cached_property on the SDK client, so the
            # instance attribute takes precedence for the client's lifetime.
            client.messages = PromptCachingMessages(client.messages, cache_control_param(self.cache_ttl))
        return client
