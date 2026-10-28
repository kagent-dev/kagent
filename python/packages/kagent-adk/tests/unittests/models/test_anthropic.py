"""Tests for KAgentAnthropicLlm."""

import copy
import json
from unittest import mock

import httpx2
import pytest
from anthropic import AsyncAnthropic, AsyncAnthropicFoundry
from anthropic.types import Message, TextBlock, ThinkingBlock, Usage
from google.adk.models.anthropic_llm import content_block_to_part
from google.adk.models.llm_request import LlmRequest
from google.genai import types

from kagent.adk.models._anthropic import (
    FoundryAnthropic,
    KAgentAnthropicLlm,
    PromptCachingMessages,
    cache_control_param,
    mark_prompt_cache_breakpoints,
)
from kagent.adk.models._azure import AI_FOUNDRY_SCOPE
from kagent.adk.types import ModelConfig


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
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic"):
            _ = llm._anthropic_client
            assert "_anthropic_client" in llm.__dict__
        llm.set_passthrough_key("new-token")
        assert "_anthropic_client" not in llm.__dict__

    def test_set_passthrough_key_preserves_cached_client_for_same_token(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", api_key_passthrough=True)
        llm.set_passthrough_key("same-token")
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic"):
            cached_client = llm._anthropic_client

        llm.set_passthrough_key("same-token")

        assert llm._anthropic_client is cached_client

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

        config = ModelConfig(
            model="claude-3-sonnet-20240229", anthropic=Anthropic(base_url="https://api.anthropic.com")
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentAnthropicLlm)
        assert result.model == "claude-3-sonnet-20240229"
        assert result.base_url == "https://api.anthropic.com"


class TestFoundryAnthropic:
    def test_model_config_dispatches_anthropic_format(self):
        from kagent.adk.types import Foundry, _create_llm_from_model_config

        config = ModelConfig(
            model="claude-haiku-4-5",
            foundry=Foundry(
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                api_format="anthropic",
            ),
        )

        result = _create_llm_from_model_config(config)

        assert isinstance(result, FoundryAnthropic)
        assert result.model == "claude-haiku-deployment"
        assert result._resolve_model_name("wrong-request-model") == "claude-haiku-deployment"

    def test_model_config_defaults_to_openai_format(self):
        from kagent.adk.models._openai import FoundryOpenAI
        from kagent.adk.types import Foundry, _create_llm_from_model_config

        result = _create_llm_from_model_config(
            ModelConfig(
                model="gpt-4.1",
                foundry=Foundry(
                    endpoint="https://example.cognitiveservices.azure.com/", deployment="gpt-4.1-deployment"
                ),
            )
        )

        assert isinstance(result, FoundryOpenAI)

    def test_workload_identity_uses_ai_foundry_scope(self):
        token_provider = object()
        with (
            mock.patch.dict("os.environ", {}, clear=True),
            mock.patch("kagent.adk.models._azure.AsyncAnthropicFoundry") as mock_anthropic,
            mock.patch(
                "kagent.adk.models._azure.azure_ad_token_provider",
                return_value=token_provider,
            ) as mock_provider,
        ):
            llm = FoundryAnthropic(
                model="claude-haiku-deployment",
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                extra_headers={"Authorization": "Bearer leaked", "X-Custom": "preserved"},
            )
            _ = llm._anthropic_client

        mock_provider.assert_called_once_with(AI_FOUNDRY_SCOPE)
        assert mock_anthropic.call_args.kwargs["azure_ad_token_provider"] is token_provider
        assert mock_anthropic.call_args.kwargs["api_key"] is None
        assert mock_anthropic.call_args.kwargs["default_headers"] == {"X-Custom": "preserved"}
        assert mock_anthropic.call_args.kwargs["base_url"] == "https://example.services.ai.azure.com/anthropic"

    def test_passthrough_without_token_does_not_fall_back_to_workload_identity(self):
        with (
            mock.patch.dict("os.environ", {"FOUNDRY_API_KEY": "must-not-win"}, clear=True),
            mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
        ):
            llm = FoundryAnthropic(
                model="claude-haiku-deployment",
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                api_key_passthrough=True,
            )

            with pytest.raises(ValueError, match="provide the passthrough token"):
                _ = llm._anthropic_client

        mock_provider.assert_not_called()

    def test_passthrough_token_change_rebuilds_foundry_client(self):
        llm = FoundryAnthropic(
            model="claude-haiku-deployment",
            endpoint="https://example.services.ai.azure.com/",
            deployment="claude-haiku-deployment",
            api_key_passthrough=True,
        )
        with mock.patch("kagent.adk.models._azure.AsyncAnthropicFoundry") as mock_anthropic:
            mock_anthropic.side_effect = [
                mock.MagicMock(spec=AsyncAnthropicFoundry),
                mock.MagicMock(spec=AsyncAnthropicFoundry),
            ]
            llm.set_passthrough_key("first-token")
            first_client = llm._anthropic_client

            llm.set_passthrough_key("second-token")
            second_client = llm._anthropic_client

        assert second_client is not first_client
        assert mock_anthropic.call_count == 2
        assert mock_anthropic.call_args.kwargs["api_key"] == "second-token"

    @pytest.mark.asyncio
    @pytest.mark.parametrize("passthrough", [False, True], ids=["environment-key", "passthrough-key"])
    async def test_api_key_uses_messages_path_and_x_api_key(self, passthrough):
        captured_request = None

        async def handler(request: httpx2.Request) -> httpx2.Response:
            nonlocal captured_request
            captured_request = request
            return httpx2.Response(
                200,
                json={
                    "id": "msg_1",
                    "type": "message",
                    "role": "assistant",
                    "model": "claude-haiku-deployment",
                    "content": [{"type": "text", "text": "ok"}],
                    "stop_reason": "end_turn",
                    "usage": {"input_tokens": 1, "output_tokens": 1},
                },
            )

        http_client = httpx2.AsyncClient(transport=httpx2.MockTransport(handler))
        with (
            mock.patch.dict("os.environ", {"FOUNDRY_API_KEY": "foundry-key"}, clear=True),
            mock.patch.object(FoundryAnthropic, "_create_http_client", return_value=http_client),
            mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
        ):
            llm = FoundryAnthropic(
                model="claude-haiku-deployment",
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                api_key_passthrough=passthrough,
                extra_headers={"Authorization": "Bearer leaked", "X-Custom": "preserved"},
            )
            if passthrough:
                llm.set_passthrough_key("passthrough-key")
            await llm._anthropic_client.messages.create(
                model=llm._resolve_model_name("wrong-request-model"),
                max_tokens=16,
                messages=[{"role": "user", "content": "hello"}],
            )
            await llm._anthropic_client.close()

        assert captured_request is not None
        assert captured_request.url.path == "/anthropic/v1/messages"
        expected_key = "passthrough-key" if passthrough else "foundry-key"
        assert captured_request.headers["x-api-key"] == expected_key
        assert captured_request.headers["api-key"] == expected_key
        assert "authorization" not in captured_request.headers
        assert captured_request.headers["x-custom"] == "preserved"
        mock_provider.assert_not_called()

    @pytest.mark.asyncio
    @pytest.mark.parametrize(
        "environment",
        [{}, {"ANTHROPIC_API_KEY": "unrelated-key"}, {"ANTHROPIC_FOUNDRY_API_KEY": "must-not-win"}],
        ids=["no-api-key", "anthropic-api-key", "anthropic-foundry-api-key"],
    )
    async def test_workload_identity_uses_bearer_without_api_keys(self, environment):
        captured_requests = []

        async def handler(request: httpx2.Request) -> httpx2.Response:
            captured_requests.append(request)
            return httpx2.Response(
                200,
                json={
                    "id": "msg_1",
                    "type": "message",
                    "role": "assistant",
                    "model": "claude-haiku-deployment",
                    "content": [{"type": "text", "text": "ok"}],
                    "stop_reason": "end_turn",
                    "usage": {"input_tokens": 1, "output_tokens": 1},
                },
            )

        http_client = httpx2.AsyncClient(transport=httpx2.MockTransport(handler))
        token_provider = mock.AsyncMock(side_effect=["entra-token", "refreshed-entra-token"])
        with (
            mock.patch.dict("os.environ", environment, clear=True),
            mock.patch.object(FoundryAnthropic, "_create_http_client", return_value=http_client),
            mock.patch(
                "kagent.adk.models._azure.azure_ad_token_provider",
                return_value=token_provider,
            ),
        ):
            llm = FoundryAnthropic(
                model="claude-haiku-deployment",
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                extra_headers={"X-Api-Key": "leaked", "api-key": "leaked", "X-Custom": "preserved"},
            )
            for _ in range(2):
                await llm._anthropic_client.messages.create(
                    model=llm._resolve_model_name(None),
                    max_tokens=16,
                    messages=[{"role": "user", "content": "hello"}],
                )
            await llm._anthropic_client.close()

        assert len(captured_requests) == 2
        for request, token in zip(captured_requests, ["entra-token", "refreshed-entra-token"], strict=True):
            assert request.url.path == "/anthropic/v1/messages"
            assert request.headers["authorization"] == f"Bearer {token}"
            assert "x-api-key" not in request.headers
            assert "api-key" not in request.headers
            assert request.headers["x-custom"] == "preserved"
        assert token_provider.await_count == 2


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

        config = ModelConfig(model="claude-sonnet-4-6", anthropic=Anthropic(prompt_caching=True, cache_ttl="1h"))
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
            before = copy.deepcopy(kwargs)
            mark_prompt_cache_breakpoints(kwargs, self.CC)
            assert kwargs == before

    def test_does_not_mutate_caller_objects(self):
        kwargs = self._agent_loop_kwargs()
        tools, messages = kwargs["tools"], kwargs["messages"]
        before_tools, before_messages = copy.deepcopy(tools), copy.deepcopy(messages)
        mark_prompt_cache_breakpoints(kwargs, self.CC)
        assert tools == before_tools
        assert messages == before_messages


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

    async def _run(self, llm):
        create = mock.AsyncMock(return_value=self._message())
        client = mock.MagicMock(spec=AsyncAnthropic)
        client.messages = mock.MagicMock()
        client.messages.create = create
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic", return_value=client):
            responses = [r async for r in llm.generate_content_async(self._request())]
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
        """Regression guard for the google-adk floor: cache reads must surface as cached tokens.

        google-adk folds Anthropic's cache_read/cache_creation counts into
        prompt_token_count and reports the read portion as
        cached_content_token_count; that is what the UI's usage view shows.
        """
        _, responses = await self._run(KAgentAnthropicLlm(model="claude-sonnet-4-6", prompt_caching=True))
        usage = responses[-1].usage_metadata
        assert usage.prompt_token_count == 1000
        assert usage.cached_content_token_count == 900
        assert usage.candidates_token_count == 5
