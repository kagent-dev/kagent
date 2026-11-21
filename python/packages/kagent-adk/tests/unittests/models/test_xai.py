"""Tests for KAgentXAILlm."""

import json
from unittest import mock

import httpx
import pytest
from openai import AsyncOpenAI

from kagent.adk.models._xai import DEFAULT_XAI_BASE_URL, KAgentXAILlm
from kagent.adk.types import ModelConfig


class TestKAgentXAILlm:
    def test_default_construction(self):
        llm = KAgentXAILlm(type="xai", model="grok-4.7", api_key="xai-test")
        assert llm.model == "grok-4.7"
        assert llm.base_url is None
        assert llm.default_headers is None
        assert llm.api_key_passthrough is None

    def test_supported_models_regex_covers_grok(self):
        assert KAgentXAILlm.supported_models() == [r"grok-.*"]

    def test_client_uses_default_base_url_when_unset(self, monkeypatch):
        monkeypatch.delenv("XAI_API_BASE", raising=False)
        llm = KAgentXAILlm(type="xai", model="grok-4.7", api_key="xai-test")
        with mock.patch("kagent.adk.models._xai.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["base_url"] == DEFAULT_XAI_BASE_URL
            assert mock_client.call_args.kwargs["api_key"] == "xai-test"

    def test_client_uses_env_base_url_when_config_unset(self, monkeypatch):
        monkeypatch.setenv("XAI_API_BASE", "https://gateway.example.com/xai/v1")
        llm = KAgentXAILlm(type="xai", model="grok-4.7", api_key="xai-test")
        with mock.patch("kagent.adk.models._xai.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["base_url"] == "https://gateway.example.com/xai/v1"

    def test_config_base_url_wins_over_env(self, monkeypatch):
        monkeypatch.setenv("XAI_API_BASE", "https://from-env.example.com/v1")
        llm = KAgentXAILlm(
            type="xai", model="grok-4.7", api_key="xai-test", base_url="https://from-config.example.com/v1"
        )
        with mock.patch("kagent.adk.models._xai.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["base_url"] == "https://from-config.example.com/v1"

    def test_client_reads_env_api_key(self, monkeypatch):
        monkeypatch.setenv("XAI_API_KEY", "env-key")
        llm = KAgentXAILlm(type="xai", model="grok-4.7")
        with mock.patch("kagent.adk.models._xai.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs["api_key"] == "env-key"

    def test_client_raises_when_no_key_and_no_passthrough(self, monkeypatch):
        monkeypatch.delenv("XAI_API_KEY", raising=False)
        llm = KAgentXAILlm(type="xai", model="grok-4.7")
        with pytest.raises(ValueError, match="xAI API key must be provided"):
            _ = llm._client

    def test_client_allows_passthrough_without_key(self, monkeypatch):
        monkeypatch.delenv("XAI_API_KEY", raising=False)
        llm = KAgentXAILlm(type="xai", model="grok-4.7", api_key_passthrough=True)
        with mock.patch("kagent.adk.models._xai.AsyncOpenAI") as mock_client:
            mock_client.return_value = mock.MagicMock(spec=AsyncOpenAI)
            _ = llm._client
            assert mock_client.call_args.kwargs.get("api_key") is None

    @pytest.mark.asyncio
    async def test_chat_completion_posts_to_xai_with_bearer_key(self, monkeypatch):
        """No live key: a mock transport stands in for api.x.ai at the default base URL."""
        monkeypatch.setenv("XAI_API_KEY", "xai-test")
        monkeypatch.delenv("XAI_API_BASE", raising=False)
        seen: list[httpx.Request] = []

        def handler(request: httpx.Request) -> httpx.Response:
            seen.append(request)
            return httpx.Response(
                200,
                json={
                    "id": "chatcmpl-test",
                    "object": "chat.completion",
                    "created": 0,
                    "model": "grok-4.7",
                    "choices": [
                        {"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}
                    ],
                },
            )

        llm = KAgentXAILlm(type="xai", model="grok-4.7")
        http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        with mock.patch.object(KAgentXAILlm, "_create_http_client", return_value=http_client):
            client = llm._client
        try:
            response = await client.chat.completions.create(
                model="grok-4.7", messages=[{"role": "user", "content": "hello"}]
            )
        finally:
            await client.close()

        assert response.choices[0].message.content == "ok"
        assert len(seen) == 1
        assert seen[0].method == "POST"
        assert str(seen[0].url) == "https://api.x.ai/v1/chat/completions"
        assert seen[0].headers["authorization"] == "Bearer xai-test"
        assert json.loads(seen[0].content)["model"] == "grok-4.7"


class TestCreateLLMFromXAIConfig:
    def test_create_llm_from_xai_model_config(self):
        from kagent.adk.types import XAI, _create_llm_from_model_config

        config = ModelConfig(
            model="grok-4.7",
            xai=XAI(base_url="https://api.x.ai/v1", temperature=0.5, max_tokens=1024),
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentXAILlm)
        assert result.model == "grok-4.7"
        assert result.base_url == "https://api.x.ai/v1"
        assert result.temperature == 0.5
        # xAI deprecates max_tokens, so the limit is sent as max_completion_tokens.
        assert result.max_completion_tokens == 1024
        assert result.max_tokens is None
