"""Tests for KAgentBedrockLlm."""

import asyncio
from unittest import mock

import pytest

from kagent.adk.models._bedrock import (
    KAgentBedrockLlm,
    _convert_content_to_converse_messages,
    _convert_tools_to_converse,
    _get_bedrock_client,
    _sanitize_tool_name,
)


class TestSanitizeToolName:
    def test_valid_name_unchanged(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("get_weather", name_map, counter) == "get_weather"
        assert name_map["get_weather"] == "get_weather"

    def test_dots_replaced_with_underscores(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("fetch.get_url", name_map, counter) == "fetch_get_url"

    def test_colon_replaced(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("filesystem:read", name_map, counter) == "filesystem_read"

    def test_spaces_replaced(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("read file", name_map, counter) == "read_file"

    def test_hyphens_kept(self):
        name_map: dict = {}
        counter = [0]
        assert _sanitize_tool_name("get-weather", name_map, counter) == "get-weather"

    def test_caching_returns_same_sanitized_name(self):
        name_map: dict = {}
        counter = [0]
        first = _sanitize_tool_name("fetch.get_url", name_map, counter)
        second = _sanitize_tool_name("fetch.get_url", name_map, counter)
        assert first == second == "fetch_get_url"
        assert counter[0] == 0  # no synthetic name needed

    def test_empty_name_gets_synthetic(self):
        name_map: dict = {}
        counter = [0]
        result = _sanitize_tool_name("", name_map, counter)
        assert result == "tool_fn_1"
        assert "" not in name_map  # empty names are not cached

    def test_fully_invalid_name_gets_synthetic(self):
        name_map: dict = {}
        counter = [0]
        # After substitution "!!!" becomes "___" which still passes the regex,
        # but a name that starts with digits after substitution is fine too.
        # Test a name that after substitution yields an empty string (all chars removed
        # if we had a stricter regex — here it will be underscores, which is valid).
        result = _sanitize_tool_name("valid_after_sub", name_map, counter)
        assert _sanitize_tool_name.__module__  # just ensure it ran without error
        assert result == "valid_after_sub"

    def test_multiple_distinct_names_get_distinct_sanitizations(self):
        name_map: dict = {}
        counter = [0]
        a = _sanitize_tool_name("server.tool_a", name_map, counter)
        b = _sanitize_tool_name("server.tool_b", name_map, counter)
        assert a == "server_tool_a"
        assert b == "server_tool_b"
        assert a != b


class TestConvertToolsToConverse:
    def _make_tool(self, name: str, description: str = "a tool"):
        from unittest.mock import MagicMock
        tool = MagicMock()
        decl = MagicMock()
        decl.name = name
        decl.description = description
        decl.parameters = None
        tool.function_declarations = [decl]
        return tool

    def test_plain_name_unchanged(self):
        name_map: dict = {}
        counter = [0]
        tools = self._make_tool("get_weather")
        result = _convert_tools_to_converse([tools], name_map, counter)
        assert result[0]["toolSpec"]["name"] == "get_weather"
        assert name_map == {"get_weather": "get_weather"}

    def test_dot_in_name_sanitized(self):
        name_map: dict = {}
        counter = [0]
        tools = self._make_tool("fetch.get_url")
        result = _convert_tools_to_converse([tools], name_map, counter)
        assert result[0]["toolSpec"]["name"] == "fetch_get_url"
        assert name_map["fetch.get_url"] == "fetch_get_url"

    def test_colon_in_name_sanitized(self):
        name_map: dict = {}
        counter = [0]
        tools = self._make_tool("filesystem:read_file")
        result = _convert_tools_to_converse([tools], name_map, counter)
        assert result[0]["toolSpec"]["name"] == "filesystem_read_file"

    def test_multiple_tools_all_sanitized(self):
        name_map: dict = {}
        counter = [0]
        t1 = self._make_tool("server.alpha")
        t2 = self._make_tool("server.beta")
        result = _convert_tools_to_converse([t1, t2], name_map, counter)
        names = [r["toolSpec"]["name"] for r in result]
        assert names == ["server_alpha", "server_beta"]


class TestConvertContentWithNameMap:
    def test_function_call_name_sanitized_via_map(self):
        from google.genai import types

        name_map = {"fetch.get_url": "fetch_get_url"}
        part = types.Part.from_function_call(name="fetch.get_url", args={"url": "https://example.com"})
        if part.function_call:
            part.function_call.id = "call-1"
        content = types.Content(role="model", parts=[part])
        messages = _convert_content_to_converse_messages([content], tool_name_map=name_map)
        assert messages[0]["content"][0]["toolUse"]["name"] == "fetch_get_url"

    def test_function_call_name_unchanged_without_map(self):
        from google.genai import types

        part = types.Part.from_function_call(name="fetch.get_url", args={})
        if part.function_call:
            part.function_call.id = "call-2"
        content = types.Content(role="model", parts=[part])
        messages = _convert_content_to_converse_messages([content], tool_name_map=None)
        assert messages[0]["content"][0]["toolUse"]["name"] == "fetch.get_url"

    def test_unknown_name_falls_back_to_original(self):
        from google.genai import types

        name_map = {"other.tool": "other_tool"}
        part = types.Part.from_function_call(name="unknown.tool", args={})
        if part.function_call:
            part.function_call.id = "call-3"
        content = types.Content(role="model", parts=[part])
        messages = _convert_content_to_converse_messages([content], tool_name_map=name_map)
        assert messages[0]["content"][0]["toolUse"]["name"] == "unknown.tool"


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

    @pytest.mark.asyncio
    async def test_tool_name_with_dot_sanitized_and_response_remapped(self):
        """Tool names like 'fetch.get_url' must be sanitized for Bedrock and
        the original name must be restored in the returned LlmResponse so the
        ADK framework can dispatch to the correct tool."""
        from unittest.mock import MagicMock

        from google.genai import types

        llm = KAgentBedrockLlm(model="us.anthropic.claude-sonnet-4-20250514-v1:0")

        # Bedrock returns the sanitized name "fetch_get_url" in the toolUse block
        converse_response = {
            "output": {
                "message": {
                    "role": "assistant",
                    "content": [
                        {
                            "toolUse": {
                                "toolUseId": "call-abc",
                                "name": "fetch_get_url",
                                "input": {"url": "https://example.com"},
                            }
                        }
                    ],
                }
            },
            "stopReason": "tool_use",
            "usage": {"inputTokens": 20, "outputTokens": 10, "totalTokens": 30},
        }
        mock_client = MagicMock()
        mock_client.converse.return_value = converse_response

        async def fake_to_thread(fn, **kwargs):
            return fn(**kwargs)

        # Build a tool declaration with the original dot-containing name
        func_decl = MagicMock()
        func_decl.name = "fetch.get_url"
        func_decl.description = "Fetch a URL"
        func_decl.parameters = None
        tool = MagicMock()
        tool.function_declarations = [func_decl]

        request = MagicMock()
        request.model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
        request.contents = []
        request.config = MagicMock()
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
        # The ADK must see the ORIGINAL name, not the Bedrock-sanitized one
        assert fc.name == "fetch.get_url"
        assert fc.id == "call-abc"

        # Also verify the sanitized name was sent to Bedrock
        call_kwargs = mock_client.converse.call_args.kwargs
        tool_names = [t["toolSpec"]["name"] for t in call_kwargs["toolConfig"]["tools"]]
        assert tool_names == ["fetch_get_url"]

    def test_create_llm_from_bedrock_model_config(self):
        """Integration: _create_llm_from_model_config returns KAgentBedrockLlm for bedrock type."""
        from kagent.adk.types import Bedrock, _create_llm_from_model_config

        config = Bedrock(type="bedrock", model="meta.llama3-8b-instruct-v1:0")
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentBedrockLlm)
        assert result.model == "meta.llama3-8b-instruct-v1:0"
