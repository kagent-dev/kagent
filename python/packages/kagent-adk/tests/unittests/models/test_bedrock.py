"""Tests for KAgentBedrockLlm."""

import asyncio
from unittest import mock

import pytest

from kagent.adk.models._bedrock import KAgentBedrockLlm, _get_bedrock_client, _inject_bearer_token


class TestGetBedrockClient:
    def test_uses_aws_default_region_env(self):
        with mock.patch.dict("os.environ", {"AWS_DEFAULT_REGION": "eu-west-1"}):
            with mock.patch("kagent.adk.models._bedrock.boto3.client") as mock_boto:
                _get_bedrock_client()
                assert mock_boto.call_args.kwargs["region_name"] == "eu-west-1"

    def test_falls_back_to_aws_region_env(self):
        env = {k: v for k, v in __import__("os").environ.items() if k != "AWS_DEFAULT_REGION"}
        env["AWS_REGION"] = "ap-southeast-1"
        with mock.patch.dict("os.environ", env, clear=True):
            with mock.patch("kagent.adk.models._bedrock.boto3.client") as mock_boto:
                _get_bedrock_client()
                assert mock_boto.call_args.kwargs["region_name"] == "ap-southeast-1"

    def test_defaults_to_us_east_1(self):
        env = {k: v for k, v in __import__("os").environ.items() if k not in ("AWS_DEFAULT_REGION", "AWS_REGION")}
        with mock.patch.dict("os.environ", env, clear=True):
            with mock.patch("kagent.adk.models._bedrock.boto3.client") as mock_boto:
                _get_bedrock_client()
                assert mock_boto.call_args.kwargs["region_name"] == "us-east-1"

    def test_bearer_token_uses_unsigned_config(self):
        """When AWS_BEARER_TOKEN_BEDROCK is set, client uses UNSIGNED signature."""
        from botocore import UNSIGNED

        env = {k: v for k, v in __import__("os").environ.items() if k not in ("AWS_DEFAULT_REGION", "AWS_REGION")}
        env["AWS_BEARER_TOKEN_BEDROCK"] = "test-token-123"
        with mock.patch.dict("os.environ", env, clear=True):
            with mock.patch("kagent.adk.models._bedrock.boto3.client") as mock_boto:
                mock_client = mock.MagicMock()
                mock_boto.return_value = mock_client
                _get_bedrock_client()
                config = mock_boto.call_args.kwargs["config"]
                assert config.signature_version == UNSIGNED

    def test_bearer_token_registers_event_handler(self):
        """When AWS_BEARER_TOKEN_BEDROCK is set, a before-sign handler is registered."""
        env = {k: v for k, v in __import__("os").environ.items() if k not in ("AWS_DEFAULT_REGION", "AWS_REGION")}
        env["AWS_BEARER_TOKEN_BEDROCK"] = "test-token-123"
        with mock.patch.dict("os.environ", env, clear=True):
            with mock.patch("kagent.adk.models._bedrock.boto3.client") as mock_boto:
                mock_client = mock.MagicMock()
                mock_boto.return_value = mock_client
                _get_bedrock_client()
                mock_client.meta.events.register.assert_called_once()
                call_args = mock_client.meta.events.register.call_args
                assert call_args[0][0] == "before-sign.bedrock-runtime.*"

    def test_no_bearer_token_uses_standard_auth(self):
        """When AWS_BEARER_TOKEN_BEDROCK is not set, standard credential chain is used."""
        env = {k: v for k, v in __import__("os").environ.items() if k not in ("AWS_DEFAULT_REGION", "AWS_REGION", "AWS_BEARER_TOKEN_BEDROCK")}
        with mock.patch.dict("os.environ", env, clear=True):
            with mock.patch("kagent.adk.models._bedrock.boto3.client") as mock_boto:
                mock_client = mock.MagicMock()
                mock_boto.return_value = mock_client
                _get_bedrock_client()
                assert "config" not in mock_boto.call_args.kwargs
                mock_client.meta.events.register.assert_not_called()


class TestInjectBearerToken:
    def test_injects_authorization_header(self):
        """_inject_bearer_token sets the correct Authorization header."""
        mock_request = mock.MagicMock()
        mock_request.headers = {}
        _inject_bearer_token("my-secret-token", mock_request)
        assert mock_request.headers["Authorization"] == "Bearer my-secret-token"


class TestKAgentBedrockLlm:
    def test_default_construction(self):
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")
        assert llm.model == "us.anthropic.claude-sonnet-4-20250514-v1:0"

    def test_non_anthropic_model_accepted(self):
        llm = KAgentBedrockLlm(model="meta.llama3-8b-instruct-v1:0")
        assert llm.model == "meta.llama3-8b-instruct-v1:0"

    @pytest.mark.asyncio
    async def test_generate_calls_converse(self):
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")
        converse_response = {
            "output": {"message": {"role": "assistant", "content": [{"text": "hello"}]}},
            "stopReason": "end_turn",
            "usage": {"inputTokens": 10, "outputTokens": 5, "totalTokens": 15},
        }
        mock_client = mock.MagicMock()
        mock_client.converse.return_value = converse_response

        async def fake_to_thread(fn, **kwargs):
            return fn(**kwargs)

        request = mock.MagicMock()
        request.model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
        request.contents = []
        request.config = None

        with (
            mock.patch("kagent.adk.models._bedrock._get_bedrock_client", return_value=mock_client),
            mock.patch("kagent.adk.models._bedrock.asyncio.to_thread", side_effect=fake_to_thread),
        ):
            responses = [r async for r in llm.generate_content_async(request)]

        assert len(responses) == 1
        assert responses[0].content.parts[0].text == "hello"

    @pytest.mark.asyncio
    async def test_streaming_captures_usage_metadata(self):
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")

        stream_events = [
            {"contentBlockStart": {"start": {}}},
            {"contentBlockDelta": {"delta": {"text": "hello"}}},
            {"messageStop": {"stopReason": "end_turn"}},
            {"metadata": {"usage": {"inputTokens": 10, "outputTokens": 5, "totalTokens": 15}}},
        ]
        mock_client = mock.MagicMock()
        mock_client.converse_stream.return_value = {"stream": stream_events}

        async def fake_to_thread(fn, **kwargs):
            return fn(**kwargs)

        request = mock.MagicMock()
        request.model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
        request.contents = []
        request.config = None

        with (
            mock.patch("kagent.adk.models._bedrock._get_bedrock_client", return_value=mock_client),
            mock.patch("kagent.adk.models._bedrock.asyncio.to_thread", side_effect=fake_to_thread),
        ):
            responses = [r async for r in llm.generate_content_async(request, stream=True)]

        final = responses[-1]
        assert final.usage_metadata is not None
        assert final.usage_metadata.prompt_token_count == 10
        assert final.usage_metadata.candidates_token_count == 5
        assert final.usage_metadata.total_token_count == 15

    def test_create_llm_from_bedrock_model_config(self):
        """Integration: _create_llm_from_model_config returns KAgentBedrockLlm for bedrock type."""
        from kagent.adk.types import Bedrock, _create_llm_from_model_config

        config = Bedrock(type="bedrock", model="meta.llama3-8b-instruct-v1:0")
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentBedrockLlm)
        assert result.model == "meta.llama3-8b-instruct-v1:0"
