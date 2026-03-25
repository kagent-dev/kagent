"""Tests for KAgentOllamaLlm (native Ollama SDK)."""

import os
from unittest import mock

import pytest

from kagent.adk.models._ollama import KAgentOllamaLlm, create_ollama_llm


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

        config = Ollama(
            type="ollama",
            model="llama3.2:latest",
            options={"temperature": "0.8", "top_p": "0.9"},
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentOllamaLlm)
        assert result.model == "llama3.2:latest"
        # Options are type-coerced by _convert_ollama_options before reaching create_ollama_llm
        assert result.ollama_options["temperature"] == 0.8
        assert result.ollama_options["top_p"] == 0.9
