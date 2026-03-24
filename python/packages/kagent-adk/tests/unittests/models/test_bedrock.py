"""Tests for KAgentBedrockLlm."""

from unittest import mock

from anthropic import AsyncAnthropicBedrock

from kagent.adk.models._bedrock import KAgentBedrockLlm


class TestKAgentBedrockLlm:
    def test_default_construction(self):
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")
        assert llm.model == "us.anthropic.claude-sonnet-4-20250514-v1:0"

    def test_construction_with_headers(self):
        llm = KAgentBedrockLlm(
            model="us.anthropic.claude-sonnet-4-20250514-v1:0",
            extra_headers={"X-Custom": "value"},
        )
        assert llm.extra_headers == {"X-Custom": "value"}

    def test_client_created(self):
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")
        with mock.patch("kagent.adk.models._bedrock.AsyncAnthropicBedrock") as mock_bedrock:
            mock_bedrock.return_value = mock.MagicMock(spec=AsyncAnthropicBedrock)
            _ = llm._anthropic_client
            assert mock_bedrock.called

    def test_client_uses_extra_headers(self):
        llm = KAgentBedrockLlm(
            model="us.anthropic.claude-sonnet-4-20250514-v1:0",
            extra_headers={"X-Amz-Custom": "val"},
        )
        with mock.patch("kagent.adk.models._bedrock.AsyncAnthropicBedrock") as mock_bedrock:
            mock_bedrock.return_value = mock.MagicMock(spec=AsyncAnthropicBedrock)
            _ = llm._anthropic_client
            assert mock_bedrock.call_args.kwargs.get("default_headers") == {"X-Amz-Custom": "val"}

    def test_client_no_headers_by_default(self):
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")
        with mock.patch("kagent.adk.models._bedrock.AsyncAnthropicBedrock") as mock_bedrock:
            mock_bedrock.return_value = mock.MagicMock(spec=AsyncAnthropicBedrock)
            _ = llm._anthropic_client
            assert "default_headers" not in mock_bedrock.call_args.kwargs

    def test_create_llm_from_bedrock_model_config(self):
        """Integration: _create_llm_from_model_config returns KAgentBedrockLlm for bedrock type."""
        from kagent.adk.types import Bedrock, _create_llm_from_model_config

        config = Bedrock(
            type="bedrock",
            model="us.anthropic.claude-sonnet-4-20250514-v1:0",
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentBedrockLlm)
        assert result.model == "us.anthropic.claude-sonnet-4-20250514-v1:0"
