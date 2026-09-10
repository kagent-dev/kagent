"""Tests for KAgentAnthropicLlm."""

import json
from unittest import mock

import pytest
from anthropic import AsyncAnthropic
from anthropic.types import (
    Message,
    MessageDeltaUsage,
    RawContentBlockDeltaEvent,
    RawContentBlockStartEvent,
    RawMessageDeltaEvent,
    RawMessageStartEvent,
    RawMessageStopEvent,
    TextBlock,
    TextDelta,
    ThinkingBlock,
    Usage,
)
from anthropic.types.raw_message_delta_event import Delta
from google.adk.models.anthropic_llm import content_block_to_part
from google.adk.models.llm_request import LlmRequest
from google.genai import types

from kagent.adk.models._anthropic import (
    KAgentAnthropicLlm,
    PromptCachingMessages,
    cache_control_param,
    fold_cache_usage,
    mark_prompt_cache_breakpoints,
)


class TestKAgentAnthropicLlm:
    def test_default_construction(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229")
        assert llm.model == "claude-3-sonnet-20240229"
        assert llm.base_url is None
        assert llm.extra_headers is None
        assert llm.api_key_passthrough is None

    def test_set_passthrough_key(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", api_key_passthrough=True)
        llm.set_passthrough_key("sk-bearer-token")
        assert llm._api_key == "sk-bearer-token"

    def test_set_passthrough_key_invalidates_cached_client(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229")
        with mock.patch("anthropic.AsyncAnthropic"):
            _ = llm._anthropic_client
            assert "_anthropic_client" in llm.__dict__
        llm.set_passthrough_key("new-token")
        assert "_anthropic_client" not in llm.__dict__

    def test_client_uses_base_url(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", base_url="https://proxy.internal/anthropic")
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            mock_anthropic.return_value = mock.MagicMock(spec=AsyncAnthropic)
            _ = llm._anthropic_client
            assert mock_anthropic.call_args.kwargs["base_url"] == "https://proxy.internal/anthropic"

    def test_client_uses_extra_headers(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", extra_headers={"X-Org": "test-org"})
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            mock_anthropic.return_value = mock.MagicMock(spec=AsyncAnthropic)
            _ = llm._anthropic_client
            assert mock_anthropic.call_args.kwargs["default_headers"] == {"X-Org": "test-org"}

    def test_client_uses_passthrough_key(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", api_key_passthrough=True)
        llm.set_passthrough_key("sk-test-key")
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            mock_anthropic.return_value = mock.MagicMock(spec=AsyncAnthropic)
            _ = llm._anthropic_client
            assert mock_anthropic.call_args.kwargs["api_key"] == "sk-test-key"

    def test_create_llm_from_anthropic_model_config(self):
        """Integration: _create_llm_from_model_config returns KAgentAnthropicLlm for anthropic type."""
        from kagent.adk.types import Anthropic, _create_llm_from_model_config

        config = Anthropic(
            type="anthropic",
            model="claude-3-sonnet-20240229",
            base_url="https://api.anthropic.com",
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentAnthropicLlm)
        assert result.model == "claude-3-sonnet-20240229"
        assert result.base_url == "https://api.anthropic.com"


class TestAnthropicThinkingBlock:
    """Regression guard for the google-adk floor that KAgentAnthropicLlm relies on.

    KAgentAnthropicLlm inherits response decoding from google-adk's AnthropicLlm.
    Models that emit thinking blocks (Claude Sonnet 5 does so by default) return a
    ThinkingBlock, which google-adk only learned to decode in 1.32.0. On an older
    pinned version content_block_to_part raises NotImplementedError, so every
    request against such a model fails. This asserts the resolved dependency can
    decode a thinking block, catching a silent downgrade below that floor.
    """

    def test_thinking_block_decodes_to_thought_part(self):
        block = ThinkingBlock(type="thinking", thinking="working through it", signature="sig")

        part = content_block_to_part(block)

        assert part.thought is True
        assert part.text == "working through it"


class TestPromptCachingConfig:
    def test_default_construction_has_caching_off(self):
        llm = KAgentAnthropicLlm(model="claude-sonnet-4-6")
        assert llm.prompt_caching is False
        assert llm.cache_ttl is None

    def test_create_llm_forwards_prompt_caching_and_ttl(self):
        from kagent.adk.types import Anthropic, _create_llm_from_model_config

        config = Anthropic(type="anthropic", model="claude-sonnet-4-6", prompt_caching=True, cache_ttl="1h")
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentAnthropicLlm)
        assert result.prompt_caching is True
        assert result.cache_ttl == "1h"

    def test_cache_control_default_ttl_omits_ttl(self):
        assert cache_control_param() == {"type": "ephemeral"}
        # "5m" is the API default, so it is left implicit.
        assert cache_control_param("5m") == {"type": "ephemeral"}

    def test_cache_control_one_hour_ttl(self):
        assert cache_control_param("1h") == {"type": "ephemeral", "ttl": "1h"}

    def test_client_without_caching_keeps_sdk_messages_resource(self):
        llm = KAgentAnthropicLlm(model="claude-sonnet-4-6")
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            client = mock.MagicMock(spec=AsyncAnthropic)
            mock_anthropic.return_value = client
            assert llm._anthropic_client.messages is client.messages

    def test_client_with_caching_wraps_messages_resource(self):
        llm = KAgentAnthropicLlm(model="claude-sonnet-4-6", prompt_caching=True, cache_ttl="1h")
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            client = mock.MagicMock(spec=AsyncAnthropic)
            mock_anthropic.return_value = client
            messages = llm._anthropic_client.messages
        assert isinstance(messages, PromptCachingMessages)
        assert messages._cache_control == {"type": "ephemeral", "ttl": "1h"}


class TestMarkPromptCacheBreakpoints:
    """Request shaping: which blocks carry the cache_control marker."""

    CC = {"type": "ephemeral"}

    def _agent_loop_kwargs(self):
        return {
            "model": "claude-sonnet-4-6",
            "system": "You are a Kubernetes assistant.",
            "tools": [
                {"name": "get_weather", "description": "lookup weather", "input_schema": {"type": "object"}},
                {"name": "list_pods", "description": "list pods", "input_schema": {"type": "object"}},
            ],
            "messages": [
                {"role": "user", "content": [{"type": "text", "text": "list the pods"}]},
                {
                    "role": "assistant",
                    "content": [{"type": "tool_use", "id": "call-1", "name": "list_pods", "input": {}}],
                },
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call-1", "content": "pod-a"}]},
                {"role": "user", "content": [{"type": "text", "text": "anything else?"}]},
            ],
        }

    def test_marks_last_tool_system_and_latest_turn_only(self):
        kwargs = self._agent_loop_kwargs()
        mark_prompt_cache_breakpoints(kwargs, self.CC)

        assert kwargs["tools"][-1]["cache_control"] == self.CC
        assert "cache_control" not in kwargs["tools"][0]
        assert kwargs["tools"][-1]["name"] == "list_pods", "tool order must be preserved"

        assert kwargs["system"] == [
            {"type": "text", "text": "You are a Kubernetes assistant.", "cache_control": self.CC},
        ]

        assert kwargs["messages"][-1]["content"][-1]["cache_control"] == self.CC
        for message in kwargs["messages"][:-1]:
            for block in message["content"]:
                assert "cache_control" not in block, "only the latest turn carries the moving breakpoint"

    def test_marks_trailing_tool_result(self):
        kwargs = self._agent_loop_kwargs()
        kwargs["messages"] = kwargs["messages"][:3]
        mark_prompt_cache_breakpoints(kwargs, self.CC)
        block = kwargs["messages"][-1]["content"][-1]
        assert block["type"] == "tool_result"
        assert block["cache_control"] == self.CC

    def test_skips_thinking_blocks_at_the_end_of_the_turn(self):
        kwargs = self._agent_loop_kwargs()
        kwargs["messages"].append(
            {
                "role": "assistant",
                "content": [
                    {"type": "text", "text": "let me check"},
                    {"type": "thinking", "thinking": "...", "signature": "sig"},
                    {"type": "redacted_thinking", "data": "..."},
                ],
            }
        )
        mark_prompt_cache_breakpoints(kwargs, self.CC)
        content = kwargs["messages"][-1]["content"]
        assert content[0]["cache_control"] == self.CC
        assert "cache_control" not in content[1]
        assert "cache_control" not in content[2]

    def test_system_block_list_marks_last_block(self):
        kwargs = {"system": [{"type": "text", "text": "a"}, {"type": "text", "text": "b"}], "messages": []}
        mark_prompt_cache_breakpoints(kwargs, self.CC)
        assert kwargs["system"] == [
            {"type": "text", "text": "a"},
            {"type": "text", "text": "b", "cache_control": self.CC},
        ]

    def test_string_message_content_becomes_a_marked_text_block(self):
        kwargs = {"messages": [{"role": "user", "content": "hi"}]}
        mark_prompt_cache_breakpoints(kwargs, self.CC)
        assert kwargs["messages"] == [
            {"role": "user", "content": [{"type": "text", "text": "hi", "cache_control": self.CC}]}
        ]

    def test_leaves_requests_without_cacheable_content_alone(self):
        for kwargs in (
            {},
            {"system": "", "tools": [], "messages": []},
            {"messages": [{"role": "user", "content": ""}]},
        ):
            before = {k: list(v) if isinstance(v, list) else v for k, v in kwargs.items()}
            mark_prompt_cache_breakpoints(kwargs, self.CC)
            assert kwargs == before

    def test_does_not_mutate_caller_objects(self):
        kwargs = self._agent_loop_kwargs()
        tools, messages = kwargs["tools"], kwargs["messages"]
        last_tool, last_message = dict(tools[-1]), dict(messages[-1])
        mark_prompt_cache_breakpoints(kwargs, self.CC)
        assert tools[-1] == last_tool
        assert messages[-1] == last_message


class TestPromptCachingRequests:
    """End to end through google-adk's request builder with the SDK client mocked out."""

    def _request(self):
        return LlmRequest(
            model="claude-sonnet-4-6",
            contents=[
                types.Content(role="user", parts=[types.Part.from_text(text="list the pods")]),
                types.Content(
                    role="model",
                    parts=[types.Part.from_function_call(name="list_pods", args={})],
                ),
                types.Content(
                    role="user",
                    parts=[types.Part.from_function_response(name="list_pods", response={"result": "pod-a"})],
                ),
                types.Content(role="user", parts=[types.Part.from_text(text="anything else?")]),
            ],
            config=types.GenerateContentConfig(
                system_instruction="You are a Kubernetes assistant.",
                tools=[
                    types.Tool(
                        function_declarations=[
                            types.FunctionDeclaration(name="get_weather", description="lookup weather"),
                            types.FunctionDeclaration(name="list_pods", description="list pods"),
                        ]
                    )
                ],
            ),
        )

    def _message(self):
        return Message(
            id="msg_1",
            type="message",
            role="assistant",
            model="claude-sonnet-4-6",
            content=[TextBlock(type="text", text="pod-a")],
            stop_reason="end_turn",
            stop_sequence=None,
            usage=Usage(input_tokens=4, output_tokens=5, cache_read_input_tokens=900, cache_creation_input_tokens=96),
        )

    def _events(self):
        """The raw stream for the same answer: usage arrives on message_start and message_delta."""

        async def stream():
            for event in (
                RawMessageStartEvent(type="message_start", message=self._message().model_copy(update={"content": []})),
                RawContentBlockStartEvent(
                    type="content_block_start", index=0, content_block=TextBlock(type="text", text="")
                ),
                RawContentBlockDeltaEvent(
                    type="content_block_delta", index=0, delta=TextDelta(type="text_delta", text="pod-a")
                ),
                RawMessageDeltaEvent(
                    type="message_delta",
                    delta=Delta(stop_reason="end_turn", stop_sequence=None),
                    usage=MessageDeltaUsage(output_tokens=5),
                ),
                RawMessageStopEvent(type="message_stop"),
            ):
                yield event

        return stream()

    async def _run(self, llm, stream=False):
        create = mock.AsyncMock(
            side_effect=lambda **kwargs: self._events() if kwargs.get("stream") else self._message()
        )
        client = mock.MagicMock(spec=AsyncAnthropic)
        client.messages = mock.MagicMock()
        client.messages.create = create
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic", return_value=client):
            responses = [r async for r in llm.generate_content_async(self._request(), stream=stream)]
        return create.call_args.kwargs, responses

    @pytest.mark.asyncio
    async def test_disabled_sends_no_cache_control(self):
        kwargs, _ = await self._run(KAgentAnthropicLlm(model="claude-sonnet-4-6"))
        assert "cache_control" not in json.dumps(kwargs, default=str)

    @pytest.mark.asyncio
    async def test_enabled_marks_tools_system_and_latest_turn(self):
        cc = {"type": "ephemeral", "ttl": "1h"}
        kwargs, _ = await self._run(KAgentAnthropicLlm(model="claude-sonnet-4-6", prompt_caching=True, cache_ttl="1h"))

        assert json.dumps(kwargs, default=str).count('"cache_control"') == 3
        assert kwargs["tools"][-1]["name"] == "list_pods"
        assert kwargs["tools"][-1]["cache_control"] == cc
        assert kwargs["system"] == [{"type": "text", "text": "You are a Kubernetes assistant.", "cache_control": cc}]
        last = kwargs["messages"][-1]
        assert last["role"] == "user"
        assert last["content"][-1]["cache_control"] == cc

    @pytest.mark.asyncio
    async def test_cache_usage_reaches_usage_metadata(self):
        """Cache reads must surface as cached tokens; that is what the UI's usage view shows.

        google-adk 1.x reports only input_tokens for Anthropic, so the fold is
        kagent's: cache_read/cache_creation are added to prompt_token_count and
        the read portion is reported as cached_content_token_count.
        """
        _, responses = await self._run(KAgentAnthropicLlm(model="claude-sonnet-4-6", prompt_caching=True))
        usage = responses[-1].usage_metadata
        assert usage.prompt_token_count == 1000
        assert usage.cached_content_token_count == 900
        assert usage.candidates_token_count == 5
        assert usage.total_token_count == 1005

    @pytest.mark.asyncio
    async def test_streaming_cache_usage_reaches_usage_metadata(self):
        kwargs, responses = await self._run(
            KAgentAnthropicLlm(model="claude-sonnet-4-6", prompt_caching=True), stream=True
        )
        assert kwargs["stream"] is True
        assert kwargs["system"][-1]["cache_control"] == {"type": "ephemeral"}
        final = [r for r in responses if not r.partial]
        assert len(final) == 1
        usage = final[0].usage_metadata
        assert usage.prompt_token_count == 1000
        assert usage.cached_content_token_count == 900
        assert usage.candidates_token_count == 5
        assert final[0].content.parts[0].text == "pod-a"

    @pytest.mark.asyncio
    async def test_usage_is_left_to_google_adk_when_caching_is_off(self):
        _, responses = await self._run(KAgentAnthropicLlm(model="claude-sonnet-4-6"))
        usage = responses[-1].usage_metadata
        assert usage.prompt_token_count == 4
        assert usage.cached_content_token_count is None


class TestFoldCacheUsage:
    def test_folds_cache_read_and_creation_into_prompt(self):
        base = types.GenerateContentResponseUsageMetadata(
            prompt_token_count=4, candidates_token_count=5, total_token_count=9
        )
        folded = fold_cache_usage(
            base, Usage(input_tokens=4, output_tokens=5, cache_read_input_tokens=900, cache_creation_input_tokens=96)
        )
        assert folded.prompt_token_count == 1000
        assert folded.cached_content_token_count == 900
        assert folded.candidates_token_count == 5
        assert folded.total_token_count == 1005

    def test_uncached_usage_is_unchanged(self):
        base = types.GenerateContentResponseUsageMetadata(
            prompt_token_count=10, candidates_token_count=5, total_token_count=15
        )
        folded = fold_cache_usage(base, Usage(input_tokens=10, output_tokens=5))
        assert folded.prompt_token_count == 10
        assert folded.cached_content_token_count == 0
        assert folded.total_token_count == 15
