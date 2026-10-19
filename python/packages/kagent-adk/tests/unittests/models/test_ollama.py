"""Tests for KAgentOllamaLlm (native Ollama SDK)."""

import os
from unittest import mock

import pytest
from google.genai import types

from kagent.adk.models._ollama import (
    KAgentOllamaLlm,
    _convert_content_to_ollama_messages,
    _convert_tools_to_ollama,
    create_ollama_llm,
)
from kagent.adk.types import ModelConfig


def test_convert_tools_uses_adk2_json_schema():
    tool = types.Tool(
        function_declarations=[
            types.FunctionDeclaration(
                name="list_pods",
                parameters_json_schema={
                    "type": "object",
                    "properties": {"namespace": {"type": "string", "enum": ["default", "kube-system"]}},
                    "required": ["namespace"],
                },
            )
        ]
    )

    result = _convert_tools_to_ollama([tool])

    parameters = result[0].function.parameters
    assert parameters is not None
    assert parameters.required == ["namespace"]
    assert parameters.properties is not None
    assert parameters.properties["namespace"].enum == ["default", "kube-system"]


class TestKAgentOllamaLlm:
    def test_default_construction(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        assert llm.model == "llama3.2:latest"
        assert llm.ollama_options is None

    def test_client_uses_ollama_api_base_env_var(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        with mock.patch.dict(os.environ, {"OLLAMA_API_BASE": "http://ollama-svc:11434"}):
            with mock.patch("kagent.adk.models._ollama.AsyncClient") as mock_client:
                mock_client.return_value = mock.MagicMock()
                _ = llm._client
                assert mock_client.call_args.kwargs["host"] == "http://ollama-svc:11434"

    def test_client_falls_back_to_localhost(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        env = {k: v for k, v in os.environ.items() if k != "OLLAMA_API_BASE"}
        with mock.patch.dict(os.environ, env, clear=True):
            with mock.patch("kagent.adk.models._ollama.AsyncClient") as mock_client:
                mock_client.return_value = mock.MagicMock()
                _ = llm._client
                assert mock_client.call_args.kwargs["host"] == "http://localhost:11434"

    def test_client_forwards_headers(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest", default_headers={"X-Custom": "val"})
        with mock.patch("kagent.adk.models._ollama.AsyncClient") as mock_client:
            mock_client.return_value = mock.MagicMock()
            _ = llm._client
            assert mock_client.call_args.kwargs["headers"] == {"X-Custom": "val"}

    def test_ollama_options_stored(self):
        opts = {"temperature": 0.8, "top_k": 40, "num_ctx": 4096}
        llm = KAgentOllamaLlm(model="llama3.2:latest", ollama_options=opts)
        assert llm.ollama_options == opts

    @pytest.mark.asyncio
    async def test_generate_content_non_streaming(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")

        mock_response = mock.MagicMock()
        mock_response.message.content = "hello"
        mock_response.message.tool_calls = []

        mock_client = mock.AsyncMock()
        mock_client.chat = mock.AsyncMock(return_value=mock_response)

        request = mock.MagicMock()
        request.model = "llama3.2:latest"
        request.contents = []
        request.config = None

        with mock.patch.object(type(llm), "_client", new_callable=lambda: property(lambda self: mock_client)):
            responses = [r async for r in llm.generate_content_async(request, stream=False)]

        assert len(responses) == 1
        assert responses[0].content.parts[0].text == "hello"
        mock_client.chat.assert_called_once()
        call_kwargs = mock_client.chat.call_args.kwargs
        assert call_kwargs["model"] == "llama3.2:latest"
        assert call_kwargs["options"] is None

    @pytest.mark.asyncio
    async def test_generate_content_forwards_ollama_options(self):
        opts = {"temperature": 0.5, "num_ctx": 2048}
        llm = KAgentOllamaLlm(model="llama3.2:latest", ollama_options=opts)

        mock_response = mock.MagicMock()
        mock_response.message.content = "hi"
        mock_response.message.tool_calls = []

        mock_client = mock.AsyncMock()
        mock_client.chat = mock.AsyncMock(return_value=mock_response)

        request = mock.MagicMock()
        request.model = "llama3.2:latest"
        request.contents = []
        request.config = None

        with mock.patch.object(type(llm), "_client", new_callable=lambda: property(lambda self: mock_client)):
            [r async for r in llm.generate_content_async(request, stream=False)]

        assert mock_client.chat.call_args.kwargs["options"] == opts

    @pytest.mark.asyncio
    async def test_generate_content_streaming_accumulates_tool_calls_before_done_chunk(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")

        tool_call = mock.MagicMock()
        tool_call.function.name = "get_weather"
        tool_call.function.arguments = {"city": "Tokyo"}

        tool_chunk = mock.MagicMock()
        tool_chunk.message.content = ""
        tool_chunk.message.tool_calls = [tool_call]
        tool_chunk.done = False

        done_chunk = mock.MagicMock()
        done_chunk.message.content = ""
        done_chunk.message.tool_calls = None
        done_chunk.done = True
        done_chunk.done_reason = "stop"
        done_chunk.prompt_eval_count = 10
        done_chunk.eval_count = 0

        async def chunks():
            yield tool_chunk
            yield done_chunk

        mock_client = mock.AsyncMock()
        mock_client.chat = mock.AsyncMock(return_value=chunks())

        request = mock.MagicMock()
        request.model = "llama3.2:latest"
        request.contents = []
        request.config = None

        with mock.patch.object(type(llm), "_client", new_callable=lambda: property(lambda self: mock_client)):
            responses = [r async for r in llm.generate_content_async(request, stream=True)]

        assert len(responses) == 1
        final_response = responses[0]
        assert final_response.partial is False
        assert final_response.turn_complete is True
        assert len(final_response.content.parts) == 1
        function_call = final_response.content.parts[0].function_call
        assert function_call.name == "get_weather"
        assert dict(function_call.args) == {"city": "Tokyo"}


class TestConvertContentToOllamaMessages:
    def test_image_inline_data_included(self):
        content = types.Content(
            role="user",
            parts=[
                types.Part(inline_data=types.Blob(mime_type="image/png", data=b"imgdata")),
            ],
        )
        messages = _convert_content_to_ollama_messages([content])
        assert len(messages) == 1
        assert messages[0].images == [b"imgdata"]

    def test_non_image_inline_data_excluded(self):
        content = types.Content(
            role="user",
            parts=[
                types.Part(inline_data=types.Blob(mime_type="application/pdf", data=b"pdfdata")),
                types.Part(text="hello"),
            ],
        )
        messages = _convert_content_to_ollama_messages([content])
        assert len(messages) == 1
        assert not messages[0].images
        assert messages[0].content == "hello"


class TestCreateOllamaLlm:
    def test_options_forwarded_to_ollama_options(self):
        llm = create_ollama_llm(
            model="llama3.2:latest",
            options={"temperature": 0.8, "top_p": 0.9, "top_k": 40, "num_ctx": 4096},
            extra_headers={},
        )
        assert isinstance(llm, KAgentOllamaLlm)
        assert llm.ollama_options == {"temperature": 0.8, "top_p": 0.9, "top_k": 40, "num_ctx": 4096}

    def test_no_options(self):
        llm = create_ollama_llm(
            model="llama3.2:latest",
            options=None,
            extra_headers={},
        )
        assert isinstance(llm, KAgentOllamaLlm)
        assert llm.ollama_options is None

    def test_headers_forwarded(self):
        llm = create_ollama_llm(
            model="llama3.2:latest",
            options=None,
            extra_headers={"X-Custom": "val"},
        )
        assert llm.default_headers == {"X-Custom": "val"}

    def test_create_llm_from_ollama_model_config(self):
        """Integration: _create_llm_from_model_config returns KAgentOllamaLlm for ollama type."""
        from kagent.adk.types import Ollama, _create_llm_from_model_config

        config = ModelConfig(model="llama3.2:latest", ollama=Ollama(options={"temperature": "0.8", "top_p": "0.9"}))
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentOllamaLlm)
        assert result.model == "llama3.2:latest"
        # Options are type-coerced by _convert_ollama_options before reaching create_ollama_llm
        assert result.ollama_options["temperature"] == 0.8
        assert result.ollama_options["top_p"] == 0.9


class TestConvertOllamaOptions:
    """Coercion of CRD string options into the Ollama SDK's typed values."""

    def test_int_option_coerced(self):
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"num_ctx": "64000"}) == {"num_ctx": 64000}

    def test_int_option_accepts_integral_float_string(self):
        """A value like "64000.0" (which JSON/YAML tooling can produce for an int
        field) must not crash the whole agent build."""
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"num_ctx": "64000.0"}) == {"num_ctx": 64000}

    def test_float_option_coerced(self):
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"temperature": "0.7"}) == {"temperature": 0.7}

    @pytest.mark.parametrize("value", ["1", "true", "True", "YES", "on", " true "])
    def test_bool_option_truthy_spellings(self, value):
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"low_vram": value}) == {"low_vram": True}

    @pytest.mark.parametrize("value", ["0", "false", "False", "no", "off"])
    def test_bool_option_falsy_spellings(self, value):
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"low_vram": value}) == {"low_vram": False}

    def test_unparseable_value_is_forwarded_not_raised(self):
        """A malformed value is kept as a string for the SDK to report, rather
        than raising and failing agent construction."""
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"num_ctx": "not-a-number"}) == {"num_ctx": "not-a-number"}

    def test_unknown_option_kept_as_string(self):
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options({"custom_flag": "value"}) == {"custom_flag": "value"}

    def test_empty_and_none(self):
        from kagent.adk.types import _convert_ollama_options

        assert _convert_ollama_options(None) == {}
        assert _convert_ollama_options({}) == {}
