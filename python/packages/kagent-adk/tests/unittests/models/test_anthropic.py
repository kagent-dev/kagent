"""Tests for KAgentAnthropicLlm."""

from unittest import mock

import pytest
from anthropic import AsyncAnthropic
from anthropic.types import ThinkingBlock
from google.adk.models.anthropic_llm import content_block_to_part

from kagent.adk.models._anthropic import KAgentAnthropicLlm


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

    def test_set_passthrough_headers_invalidates_cached_client(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", passthrough_headers=["x-guardrail-token"])
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic"):
            _ = llm._anthropic_client
            assert "_anthropic_client" in llm.__dict__
        llm.set_passthrough_headers({"x-guardrail-token": "caller-token"})
        assert "_anthropic_client" not in llm.__dict__

    def test_set_passthrough_headers_same_value_keeps_cached_client(self):
        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229")
        llm.set_passthrough_headers({"x-guardrail-token": "caller-token"})
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic"):
            _ = llm._anthropic_client
        llm.set_passthrough_headers({"x-guardrail-token": "caller-token"})
        assert "_anthropic_client" in llm.__dict__

    def test_client_merges_passthrough_headers_over_extra_headers(self):
        llm = KAgentAnthropicLlm(
            model="claude-3-sonnet-20240229",
            extra_headers={"x-tenant": "config-tenant", "x-static": "static-value"},
        )
        llm.set_passthrough_headers({"x-tenant": "caller-tenant"})
        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            mock_anthropic.return_value = mock.MagicMock(spec=AsyncAnthropic)
            _ = llm._anthropic_client
            assert mock_anthropic.call_args.kwargs["default_headers"] == {
                "x-tenant": "caller-tenant",
                "x-static": "static-value",
            }

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

    def test_create_llm_from_anthropic_model_config_with_passthrough_headers(self):
        from kagent.adk.types import Anthropic, _create_llm_from_model_config

        config = Anthropic(
            type="anthropic",
            model="claude-3-sonnet-20240229",
            passthrough_headers=["x-guardrail-token"],
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentAnthropicLlm)
        assert result.passthrough_headers == ["x-guardrail-token"]

    @pytest.mark.asyncio
    async def test_second_caller_without_headers_does_not_inherit_previous_token(self):
        """Regression: the shared cached client must not leak caller A's pass-through header to caller B."""
        from kagent.adk._llm_header_passthrough_plugin import LLMHeaderPassthroughPlugin

        llm = KAgentAnthropicLlm(model="claude-3-sonnet-20240229", passthrough_headers=["x-guardrail-token"])
        plugin = LLMHeaderPassthroughPlugin()

        def callback_context(headers):
            ctx = mock.MagicMock()
            ctx.state = {"headers": headers}
            ctx._invocation_context.agent.model = llm
            return ctx

        llm_request = mock.MagicMock()

        with mock.patch("kagent.adk.models._anthropic.AsyncAnthropic") as mock_anthropic:
            mock_anthropic.return_value = mock.MagicMock(spec=AsyncAnthropic)

            await plugin.before_model_callback(
                callback_context=callback_context({"x-guardrail-token": "CALLER-A-TOKEN"}),
                llm_request=llm_request,
            )
            _ = llm._anthropic_client
            assert mock_anthropic.call_args.kwargs["default_headers"] == {"x-guardrail-token": "CALLER-A-TOKEN"}

            await plugin.before_model_callback(callback_context=callback_context({}), llm_request=llm_request)
            _ = llm._anthropic_client
            assert "default_headers" not in mock_anthropic.call_args.kwargs


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
