"""Tests for KAgentBedrockLlm."""

import asyncio
from unittest import mock

import pytest

from kagent.adk.models._bedrock import (
    KAgentBedrockLlm,
    _convert_tools_to_converse,
    _get_bedrock_client,
    _sanitize_tool_name,
)


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


class TestSanitizeToolName:
    def test_valid_name_unchanged(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("get_pods", name_map, counter) == "get_pods"
        assert name_map == {"get_pods": "get_pods"}

    def test_dot_replaced_with_underscore(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("kubernetes.get_pods", name_map, counter) == "kubernetes_get_pods"
        assert name_map["kubernetes.get_pods"] == "kubernetes_get_pods"

    def test_space_replaced_with_underscore(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("get pods", name_map, counter) == "get_pods"

    def test_colon_replaced_with_underscore(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("ns:tool", name_map, counter) == "ns_tool"

    def test_empty_name_gets_fallback(self):
        name_map: dict = {}
        counter = [0]
        result = _sanitize_tool_name("", name_map, counter)
        assert result == "unknown_tool_1"
        assert counter[0] == 1

    def test_fully_invalid_name_becomes_underscores(self):
        name_map: dict = {}
        counter = [0]
        result = _sanitize_tool_name("!@#$", name_map, counter)
        # Characters are replaced with _ so the result is still valid per pattern
        assert result == "____"
        assert counter[0] == 0

    def test_same_name_returns_cached_sanitized(self):
        name_map: dict = {}
        counter = [0]
        first = _sanitize_tool_name("mcp.server.tool", name_map, counter)
        second = _sanitize_tool_name("mcp.server.tool", name_map, counter)
        assert first == second == "mcp_server_tool"

    def test_hyphen_preserved(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("my-tool", name_map, counter) == "my-tool"


class TestConvertToolsToConverse:
    def test_tool_name_with_dot_is_sanitized(self):
        from google.genai import types as genai_types

        func_decl = mock.MagicMock()
        func_decl.name = "github_copilot.suggest"
        func_decl.description = "Suggest code"
        func_decl.parameters = None

        tool = mock.MagicMock()
        tool.function_declarations = [func_decl]

        name_map: dict = {}
        counter = [0]
        result = _convert_tools_to_converse([tool], name_map, counter)

        assert result[0]["toolSpec"]["name"] == "github_copilot_suggest"
        assert name_map["github_copilot.suggest"] == "github_copilot_suggest"

    def test_valid_tool_name_unchanged(self):
        func_decl = mock.MagicMock()
        func_decl.name = "list_namespaces"
        func_decl.description = "List namespaces"
        func_decl.parameters = None

        tool = mock.MagicMock()
        tool.function_declarations = [func_decl]

        name_map: dict = {}
        counter = [0]
        result = _convert_tools_to_converse([tool], name_map, counter)

        assert result[0]["toolSpec"]["name"] == "list_namespaces"


class TestBedrockToolNameRoundTrip:
    @pytest.mark.asyncio
    async def test_dotted_tool_name_restored_in_non_streaming_response(self):
        """Tool names with dots are sanitized outbound and restored from the Bedrock response."""
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")

        converse_response = {
            "output": {
                "message": {
                    "role": "assistant",
                    "content": [
                        {
                            "toolUse": {
                                "toolUseId": "call_abc",
                                "name": "github_copilot_suggest",
                                "input": {"prompt": "hello"},
                            }
                        }
                    ],
                }
            },
            "stopReason": "tool_use",
            "usage": {"inputTokens": 10, "outputTokens": 5, "totalTokens": 15},
        }
        mock_client = mock.MagicMock()
        mock_client.converse.return_value = converse_response

        async def fake_to_thread(fn, **kwargs):
            return fn(**kwargs)

        from google.genai import types as genai_types

        func_decl = mock.MagicMock()
        func_decl.name = "github_copilot.suggest"
        func_decl.description = "Suggest"
        func_decl.parameters = None

        tool = mock.MagicMock(spec=genai_types.Tool)
        tool.function_declarations = [func_decl]

        request = mock.MagicMock()
        request.model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
        request.contents = []
        request.config = mock.MagicMock()
        request.config.system_instruction = None
        request.config.tools = [tool]
        request.config.temperature = None
        request.config.max_output_tokens = None
        request.config.top_p = None
        request.config.stop_sequences = None

        with (
            mock.patch("kagent.adk.models._bedrock._get_bedrock_client", return_value=mock_client),
            mock.patch("kagent.adk.models._bedrock.asyncio.to_thread", side_effect=fake_to_thread),
        ):
            responses = [r async for r in llm.generate_content_async(request)]

        assert len(responses) == 1
        fc = responses[0].content.parts[0].function_call
        assert fc.name == "github_copilot.suggest"

    @pytest.mark.asyncio
    async def test_dotted_tool_name_restored_in_streaming_response(self):
        """Tool names with dots are sanitized outbound and restored from streaming Bedrock response."""
        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")

        stream_events = [
            {
                "contentBlockStart": {
                    "start": {"toolUse": {"toolUseId": "call_xyz", "name": "github_copilot_suggest"}}
                }
            },
            {"contentBlockDelta": {"delta": {"toolUse": {"input": '{"prompt": "hi"}'}}}},
            {"messageStop": {"stopReason": "tool_use"}},
            {"metadata": {"usage": {"inputTokens": 5, "outputTokens": 3, "totalTokens": 8}}},
        ]
        mock_client = mock.MagicMock()
        mock_client.converse_stream.return_value = {"stream": stream_events}

        async def fake_to_thread(fn, **kwargs):
            return fn(**kwargs)

        func_decl = mock.MagicMock()
        func_decl.name = "github_copilot.suggest"
        func_decl.description = "Suggest"
        func_decl.parameters = None

        tool = mock.MagicMock()
        tool.function_declarations = [func_decl]

        request = mock.MagicMock()
        request.model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
        request.contents = []
        request.config = mock.MagicMock()
        request.config.system_instruction = None
        request.config.tools = [tool]
        request.config.temperature = None
        request.config.max_output_tokens = None
        request.config.top_p = None
        request.config.stop_sequences = None

        with (
            mock.patch("kagent.adk.models._bedrock._get_bedrock_client", return_value=mock_client),
            mock.patch("kagent.adk.models._bedrock.asyncio.to_thread", side_effect=fake_to_thread),
        ):
            responses = [r async for r in llm.generate_content_async(request, stream=True)]

        final = responses[-1]
        fc = final.content.parts[0].function_call
        assert fc.name == "github_copilot.suggest"
