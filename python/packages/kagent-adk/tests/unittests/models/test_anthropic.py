"""Tests for KAgentAnthropicLlm."""

from unittest import mock

import httpx
import pytest
from anthropic import AsyncAnthropic
from anthropic.lib.credentials import AccessToken
from anthropic.types import ThinkingBlock
from google.adk.models.anthropic_llm import content_block_to_part

from kagent.adk.models._anthropic import FoundryAnthropic, KAgentAnthropicLlm
from kagent.adk.models._azure import AI_FOUNDRY_SCOPE


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

        config = Anthropic(
            type="anthropic",
            model="claude-3-sonnet-20240229",
            base_url="https://api.anthropic.com",
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentAnthropicLlm)
        assert result.model == "claude-3-sonnet-20240229"
        assert result.base_url == "https://api.anthropic.com"


class TestFoundryAnthropic:
    def test_model_config_dispatches_anthropic_format(self):
        from kagent.adk.types import Foundry, _create_llm_from_model_config

        config = Foundry(
            type="foundry",
            model="claude-haiku-4-5",
            endpoint="https://example.services.ai.azure.com/",
            deployment="claude-haiku-deployment",
            api_format="anthropic",
        )

        result = _create_llm_from_model_config(config)

        assert isinstance(result, FoundryAnthropic)
        assert result.model == "claude-haiku-deployment"
        assert result._resolve_model_name("wrong-request-model") == "claude-haiku-deployment"

    def test_model_config_defaults_to_openai_format(self):
        from kagent.adk.models._openai import FoundryOpenAI
        from kagent.adk.types import Foundry, _create_llm_from_model_config

        result = _create_llm_from_model_config(
            Foundry(
                type="foundry",
                model="gpt-4.1",
                endpoint="https://example.cognitiveservices.azure.com/",
                deployment="gpt-4.1-deployment",
            )
        )

        assert isinstance(result, FoundryOpenAI)

    def test_workload_identity_uses_ai_foundry_scope(self):
        token_provider = object()
        with (
            mock.patch.dict("os.environ", {}, clear=True),
            mock.patch("kagent.adk.models._azure.AsyncAnthropic") as mock_anthropic,
            mock.patch(
                "kagent.adk.models._azure.azure_access_token_provider",
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
        assert mock_anthropic.call_args.kwargs["credentials"] is token_provider
        assert "api_key" not in mock_anthropic.call_args.kwargs
        assert mock_anthropic.call_args.kwargs["default_headers"] == {"X-Custom": "preserved"}
        assert mock_anthropic.call_args.kwargs["base_url"] == "https://example.services.ai.azure.com/anthropic"

    def test_passthrough_without_token_does_not_fall_back_to_workload_identity(self):
        with (
            mock.patch.dict("os.environ", {"FOUNDRY_API_KEY": "must-not-win"}, clear=True),
            mock.patch("kagent.adk.models._azure.azure_access_token_provider") as mock_provider,
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
        with mock.patch("kagent.adk.models._azure.AsyncAnthropic") as mock_anthropic:
            mock_anthropic.side_effect = [
                mock.MagicMock(spec=AsyncAnthropic),
                mock.MagicMock(spec=AsyncAnthropic),
            ]
            llm.set_passthrough_key("first-token")
            first_client = llm._anthropic_client

            llm.set_passthrough_key("second-token")
            second_client = llm._anthropic_client

        assert second_client is not first_client
        assert mock_anthropic.call_count == 2
        assert mock_anthropic.call_args.kwargs["api_key"] == "second-token"

    @pytest.mark.asyncio
    async def test_api_key_uses_messages_path_and_x_api_key(self):
        captured_request = None

        async def handler(request: httpx.Request) -> httpx.Response:
            nonlocal captured_request
            captured_request = request
            return httpx.Response(
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

        http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        with (
            mock.patch.dict("os.environ", {"FOUNDRY_API_KEY": "foundry-key"}, clear=True),
            mock.patch.object(FoundryAnthropic, "_create_http_client", return_value=http_client),
        ):
            llm = FoundryAnthropic(
                model="claude-haiku-deployment",
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                extra_headers={"Authorization": "Bearer leaked", "X-Custom": "preserved"},
            )
            await llm._anthropic_client.messages.create(
                model=llm._resolve_model_name("wrong-request-model"),
                max_tokens=16,
                messages=[{"role": "user", "content": "hello"}],
            )
            await llm._anthropic_client.close()

        assert captured_request is not None
        assert captured_request.url.path == "/anthropic/v1/messages"
        assert captured_request.headers["x-api-key"] == "foundry-key"
        assert "authorization" not in captured_request.headers
        assert captured_request.headers["x-custom"] == "preserved"

    @pytest.mark.asyncio
    async def test_workload_identity_uses_bearer_without_x_api_key(self):
        captured_request = None

        async def handler(request: httpx.Request) -> httpx.Response:
            nonlocal captured_request
            captured_request = request
            return httpx.Response(
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

        http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        token_provider = mock.Mock(return_value=AccessToken(token="entra-token", expires_at=4_102_444_800))
        with (
            mock.patch.dict("os.environ", {}, clear=True),
            mock.patch.object(FoundryAnthropic, "_create_http_client", return_value=http_client),
            mock.patch(
                "kagent.adk.models._azure.azure_access_token_provider",
                return_value=token_provider,
            ),
        ):
            llm = FoundryAnthropic(
                model="claude-haiku-deployment",
                endpoint="https://example.services.ai.azure.com/",
                deployment="claude-haiku-deployment",
                extra_headers={"X-Api-Key": "leaked", "X-Custom": "preserved"},
            )
            await llm._anthropic_client.messages.create(
                model=llm._resolve_model_name(None),
                max_tokens=16,
                messages=[{"role": "user", "content": "hello"}],
            )
            await llm._anthropic_client.close()

        assert captured_request is not None
        assert captured_request.url.path == "/anthropic/v1/messages"
        assert captured_request.headers["authorization"] == "Bearer entra-token"
        assert "x-api-key" not in captured_request.headers
        assert captured_request.headers["x-custom"] == "preserved"
        token_provider.assert_called_once()


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
