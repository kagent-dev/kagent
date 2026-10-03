"""Tests for KAgentMistralLlm."""

from unittest import mock

import pytest
from openai import AsyncOpenAI

from kagent.adk.models._mistral import DEFAULT_MISTRAL_BASE_URL, KAgentMistralLlm


class TestKAgentMistralLlm:
    def test_default_construction(self):
        llm = KAgentMistralLlm(type="mistral", model="mistral-large-latest", api_key="sk-test")
        assert llm.model == "mistral-large-latest"
        assert llm.base_url is None
        assert llm.default_headers is None
        assert llm.api_key_passthrough is None

    def test_supported_models_regex_covers_mistral_families(self):
        patterns = KAgentMistralLlm.supported_models()
        assert r"mistral-.*" in patterns
        assert r"magistral-.*" in patterns
        assert r"codestral-.*" in patterns
        assert r"ministral-.*" in patterns
        assert r"pixtral-.*" in patterns
        assert r"open-mistral-.*" in patterns

    def test_client_uses_default_base_url_when_unset(self, monkeypatch):
        monkeypatch.delenv("MISTRAL_API_BASE", raising=False)
        llm = KAgentMistralLlm(type="mistral", model="mistral-large-latest", api_key="sk-test")
        with mock.patch("kagent.adk.models._mistral.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["base_url"] == DEFAULT_MISTRAL_BASE_URL
            assert mock_client.call_args.kwargs["api_key"] == "sk-test"

    def test_client_uses_env_base_url_when_config_unset(self, monkeypatch):
        monkeypatch.setenv("MISTRAL_API_BASE", "https://gateway.example.com/mistral/v1")
        llm = KAgentMistralLlm(type="mistral", model="mistral-large-latest", api_key="sk-test")
        with mock.patch("kagent.adk.models._mistral.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["base_url"] == "https://gateway.example.com/mistral/v1"

    def test_config_base_url_wins_over_env(self, monkeypatch):
        monkeypatch.setenv("MISTRAL_API_BASE", "https://from-env.example.com/v1")
        llm = KAgentMistralLlm(
            type="mistral",
            model="mistral-large-latest",
            api_key="sk-test",
            base_url="https://from-config.example.com/v1",
        )
        with mock.patch("kagent.adk.models._mistral.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["base_url"] == "https://from-config.example.com/v1"

    def test_client_reads_env_api_key(self, monkeypatch):
        monkeypatch.setenv("MISTRAL_API_KEY", "env-key")
        llm = KAgentMistralLlm(type="mistral", model="mistral-medium-latest")
        with mock.patch("kagent.adk.models._mistral.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["api_key"] == "env-key"

    def test_client_raises_when_no_key_and_no_passthrough(self, monkeypatch):
        monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
        llm = KAgentMistralLlm(type="mistral", model="mistral-small-latest")
        with pytest.raises(ValueError, match="Mistral API key must be provided"):
            _ = llm._client

    def test_client_allows_passthrough_without_key(self, monkeypatch):
        monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
        llm = KAgentMistralLlm(type="mistral", model="mistral-small-latest", api_key_passthrough=True)
        with mock.patch("kagent.adk.models._mistral.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            # No API key required; call goes through with None
            assert mock_client.call_args.kwargs.get("api_key") is None

    def test_client_uses_default_headers(self):
        llm = KAgentMistralLlm(
            type="mistral",
            model="mistral-large-latest",
            api_key="sk-test",
            default_headers={"X-Org": "test-org"},
        )
        with mock.patch("kagent.adk.models._mistral.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["default_headers"] == {"X-Org": "test-org"}


class TestCreateLLMFromMistralConfig:
    def test_create_llm_from_mistral_model_config(self):
        """Integration: _create_llm_from_model_config returns KAgentMistralLlm for mistral type."""
        from kagent.adk.types import Mistral, _create_llm_from_model_config

        config = Mistral(
            type="mistral",
            model="mistral-large-latest",
            base_url="https://api.mistral.ai/v1",
            temperature=0.5,
            max_tokens=1024,
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentMistralLlm)
        assert result.model == "mistral-large-latest"
        assert result.base_url == "https://api.mistral.ai/v1"
        assert result.temperature == 0.5
        assert result.max_tokens == 1024
