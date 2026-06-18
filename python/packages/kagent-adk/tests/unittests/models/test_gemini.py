"""Tests for KAgentGeminiLlm."""

from unittest import mock

from kagent.adk.models._gemini import KAgentGeminiLlm


class TestKAgentGeminiLlm:
    def test_default_construction(self):
        llm = KAgentGeminiLlm(model="gemini-2.0-flash")
        assert llm.model == "gemini-2.0-flash"
        assert llm.extra_headers is None
        assert llm.api_key_passthrough is None
        assert llm._api_key is None

    def test_set_passthrough_key(self):
        llm = KAgentGeminiLlm(model="gemini-2.0-flash", api_key_passthrough=True)
        llm.set_passthrough_key("sk-bearer-token")
        assert llm._api_key == "sk-bearer-token"

    def test_set_passthrough_key_invalidates_cached_clients(self):
        llm = KAgentGeminiLlm(model="gemini-2.0-flash")
        with mock.patch("kagent.adk.models._gemini.Client"):
            _ = llm.api_client
            _ = llm._live_api_client
            assert "api_client" in llm.__dict__
            assert "_live_api_client" in llm.__dict__
        llm.set_passthrough_key("new-token")
        assert "api_client" not in llm.__dict__
        assert "_live_api_client" not in llm.__dict__

    def test_client_uses_passthrough_key(self):
        llm = KAgentGeminiLlm(model="gemini-2.0-flash", api_key_passthrough=True)
        llm.set_passthrough_key("sk-test-key")
        with mock.patch("kagent.adk.models._gemini.Client") as mock_client:
            _ = llm.api_client
            assert mock_client.call_args.kwargs["api_key"] == "sk-test-key"

    def test_client_falls_back_to_env(self, monkeypatch):
        monkeypatch.setenv("GOOGLE_API_KEY", "env-key")
        llm = KAgentGeminiLlm(model="gemini-2.0-flash")
        with mock.patch("kagent.adk.models._gemini.Client") as mock_client:
            _ = llm.api_client
            assert mock_client.call_args.kwargs["api_key"] == "env-key"
