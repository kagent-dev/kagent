"""Tests for KAgentOllamaLlm."""

import os
from unittest import mock

from kagent.adk.models._ollama import _UNSUPPORTED_OLLAMA_OPTIONS, KAgentOllamaLlm, create_ollama_llm


class TestKAgentOllamaLlm:
    def test_default_construction(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        assert llm.model == "llama3.2:latest"
        assert llm.temperature is None
        assert llm.top_p is None

    def test_client_uses_ollama_api_base_env_var(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        with mock.patch.dict(os.environ, {"OLLAMA_API_BASE": "http://ollama-svc:11434"}):
            with mock.patch("kagent.adk.models._ollama.AsyncOpenAI") as mock_openai:
                mock_openai.return_value = mock.MagicMock()
                _ = llm._client
                assert mock_openai.call_args.kwargs["base_url"] == "http://ollama-svc:11434/v1"

    def test_client_does_not_double_append_v1(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        with mock.patch.dict(os.environ, {"OLLAMA_API_BASE": "http://localhost:11434/v1"}):
            with mock.patch("kagent.adk.models._ollama.AsyncOpenAI") as mock_openai:
                mock_openai.return_value = mock.MagicMock()
                _ = llm._client
                assert mock_openai.call_args.kwargs["base_url"] == "http://localhost:11434/v1"

    def test_client_falls_back_to_localhost(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        env = {k: v for k, v in os.environ.items() if k != "OLLAMA_API_BASE"}
        with mock.patch.dict(os.environ, env, clear=True):
            with mock.patch("kagent.adk.models._ollama.AsyncOpenAI") as mock_openai:
                mock_openai.return_value = mock.MagicMock()
                _ = llm._client
                assert mock_openai.call_args.kwargs["base_url"] == "http://localhost:11434/v1"

    def test_client_uses_dummy_api_key(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest")
        with mock.patch("kagent.adk.models._ollama.AsyncOpenAI") as mock_openai:
            mock_openai.return_value = mock.MagicMock()
            _ = llm._client
            assert mock_openai.call_args.kwargs["api_key"] == "ollama"

    def test_set_passthrough_key(self):
        llm = KAgentOllamaLlm(model="llama3.2:latest", api_key_passthrough=True)
        llm.set_passthrough_key("bearer-token")
        assert llm.api_key == "bearer-token"


class TestCreateOllamaLlm:
    def test_temperature_and_top_p_extracted(self):
        llm = create_ollama_llm(
            model="llama3.2:latest",
            options={"temperature": 0.8, "top_p": 0.9},
            extra_headers={},
            api_key_passthrough=None,
        )
        assert isinstance(llm, KAgentOllamaLlm)
        assert llm.temperature == 0.8
        assert llm.top_p == 0.9

    def test_unsupported_options_logged_and_ignored(self, caplog):
        import logging

        with caplog.at_level(logging.WARNING, logger="kagent.adk.models._ollama"):
            llm = create_ollama_llm(
                model="llama3.2:latest",
                options={"num_ctx": 2048, "top_k": 40, "temperature": 0.5},
                extra_headers={},
                api_key_passthrough=None,
            )
        assert "num_ctx" in caplog.text
        assert "top_k" in caplog.text
        assert llm.temperature == 0.5

    def test_no_options(self):
        llm = create_ollama_llm(
            model="llama3.2:latest",
            options=None,
            extra_headers={},
            api_key_passthrough=None,
        )
        assert isinstance(llm, KAgentOllamaLlm)
        assert llm.temperature is None

    def test_headers_forwarded(self):
        llm = create_ollama_llm(
            model="llama3.2:latest",
            options=None,
            extra_headers={"X-Custom": "val"},
            api_key_passthrough=None,
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
        assert result.temperature == 0.8
        assert result.top_p == 0.9
