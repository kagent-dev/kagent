"""Tests for ModelConfig retry (max_retries) plumbing into provider SDK clients."""

import json

from kagent.adk.types import AgentConfig, _create_llm_from_model_config


def _make_model_config(**model_extra):
    config = {
        "model": {"type": "openai", "model": "gpt-4", **model_extra},
        "description": "test agent",
        "instruction": "test instruction",
    }
    return AgentConfig.model_validate_json(json.dumps(config)).model


class TestMaxRetriesParsing:
    def test_default_unset(self):
        model = _make_model_config()
        assert model.max_retries is None

    def test_max_retries_parsed(self):
        model = _make_model_config(max_retries=5)
        assert model.max_retries == 5


class TestMaxRetriesWiring:
    def test_openai_client_max_retries(self, monkeypatch):
        monkeypatch.setenv("OPENAI_API_KEY", "test-key")
        llm = _create_llm_from_model_config(_make_model_config(max_retries=5))
        assert llm.max_retries == 5
        assert llm._client.max_retries == 5

    def test_openai_client_default_when_unset(self, monkeypatch):
        monkeypatch.setenv("OPENAI_API_KEY", "test-key")
        llm = _create_llm_from_model_config(_make_model_config())
        assert llm.max_retries is None
        # OpenAI SDK default (2) applies when unset.
        assert llm._client.max_retries == 2

    def test_anthropic_client_max_retries(self, monkeypatch):
        monkeypatch.setenv("ANTHROPIC_API_KEY", "test-key")
        config = {
            "model": {"type": "anthropic", "model": "claude-sonnet-4-5", "max_retries": 4},
            "description": "d",
            "instruction": "i",
        }
        llm = AgentConfig.model_validate_json(json.dumps(config)).model
        llm = _create_llm_from_model_config(llm)
        assert llm.max_retries == 4
        assert llm._anthropic_client.max_retries == 4

    def test_gemini_retry_options(self):
        config = {
            "model": {"type": "gemini", "model": "gemini-2.0-flash", "max_retries": 3},
            "description": "d",
            "instruction": "i",
        }
        model = AgentConfig.model_validate_json(json.dumps(config)).model
        llm = _create_llm_from_model_config(model)
        # HttpRetryOptions.attempts counts the initial request too.
        assert llm.retry_options is not None
        assert llm.retry_options.attempts == 4

    def test_unsupported_provider_logs_warning(self, caplog):
        config = {
            "model": {"type": "ollama", "model": "llama3", "max_retries": 3},
            "description": "d",
            "instruction": "i",
        }
        model = AgentConfig.model_validate_json(json.dumps(config)).model
        with caplog.at_level("WARNING"):
            _create_llm_from_model_config(model)
        assert any("retry.attempts is not supported" in r.message for r in caplog.records)
