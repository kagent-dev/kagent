import json

import pytest
from pydantic import ValidationError

from kagent.adk.types import (
    AgentConfig,
    ContextCacheSettings,
    ContextCompressionSettings,
    ContextConfig,
    Gemini,
    OpenAI,
    build_adk_context_configs,
)


def _make_agent_config_json(**context_kwargs) -> str:
    config = {
        "model": {"type": "openai", "model": "gpt-4"},
        "description": "test agent",
        "instruction": "test instruction",
    }
    if context_kwargs:
        config["context_config"] = context_kwargs
    return json.dumps(config)


class TestContextConfigParsing:
    def test_no_context_config(self):
        config = AgentConfig.model_validate_json(_make_agent_config_json())
        assert config.context_config is None

    def test_empty_context_config(self):
        json_str = _make_agent_config_json()
        data = json.loads(json_str)
        data["context_config"] = {}
        config = AgentConfig.model_validate(data)
        assert config.context_config is not None
        assert config.context_config.compaction is None
        assert config.context_config.cache is None

    def test_compaction_only(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {"compaction": {"compaction_interval": 5, "overlap_size": 2}}
        config = AgentConfig.model_validate(data)
        assert config.context_config is not None
        assert config.context_config.compaction is not None
        assert config.context_config.compaction.compaction_interval == 5
        assert config.context_config.compaction.overlap_size == 2
        assert config.context_config.cache is None

    def test_cache_only(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {"cache": {"cache_intervals": 20, "ttl_seconds": 3600, "min_tokens": 100}}
        config = AgentConfig.model_validate(data)
        assert config.context_config is not None
        assert config.context_config.compaction is None
        assert config.context_config.cache is not None
        assert config.context_config.cache.cache_intervals == 20
        assert config.context_config.cache.ttl_seconds == 3600
        assert config.context_config.cache.min_tokens == 100

    def test_both_compaction_and_cache(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 10,
                "overlap_size": 3,
                "token_threshold": 1000,
                "event_retention_size": 5,
            },
            "cache": {"cache_intervals": 15},
        }
        config = AgentConfig.model_validate(data)
        assert config.context_config.compaction.compaction_interval == 10
        assert config.context_config.compaction.overlap_size == 3
        assert config.context_config.compaction.token_threshold == 1000
        assert config.context_config.compaction.event_retention_size == 5
        assert config.context_config.cache.cache_intervals == 15

    def test_compaction_with_summarizer_model(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model": {"type": "openai", "model": "gpt-4o-mini"},
                "prompt_template": "Summarize these events: {{events}}",
            }
        }
        config = AgentConfig.model_validate(data)
        comp = config.context_config.compaction
        assert isinstance(comp.summarizer_model, OpenAI)
        assert comp.summarizer_model.model == "gpt-4o-mini"
        assert comp.prompt_template == "Summarize these events: {{events}}"

    def test_compaction_with_gemini_summarizer(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model": {"type": "gemini", "model": "gemini-2.0-flash-lite"},
            }
        }
        config = AgentConfig.model_validate(data)
        comp = config.context_config.compaction
        assert isinstance(comp.summarizer_model, Gemini)
        assert comp.summarizer_model.model == "gemini-2.0-flash-lite"

    def test_compaction_without_summarizer_model(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
            }
        }
        config = AgentConfig.model_validate(data)
        assert config.context_config.compaction.summarizer_model is None

    def test_compaction_missing_required_fields(self):
        with pytest.raises(ValidationError):
            ContextCompressionSettings(compaction_interval=5)  # missing overlap_size

        with pytest.raises(ValidationError):
            ContextCompressionSettings(overlap_size=2)  # missing compaction_interval

    def test_cache_with_defaults(self):
        cache = ContextCacheSettings()
        assert cache.cache_intervals is None
        assert cache.ttl_seconds is None
        assert cache.min_tokens is None

    def test_null_vs_absent_context_config(self):
        # Absent
        data = json.loads(_make_agent_config_json())
        config1 = AgentConfig.model_validate(data)
        assert config1.context_config is None

        # Explicit null
        data["context_config"] = None
        config2 = AgentConfig.model_validate(data)
        assert config2.context_config is None


class TestBuildAdkContextConfigs:
    def test_compaction_basic(self):
        ctx_config = ContextConfig(compaction=ContextCompressionSettings(compaction_interval=5, overlap_size=2))
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is not None
        assert events_cfg.compaction_interval == 5
        assert events_cfg.overlap_size == 2
        assert events_cfg.summarizer is None
        assert cache_cfg is None

    def test_cache_basic(self):
        ctx_config = ContextConfig(cache=ContextCacheSettings(cache_intervals=20, ttl_seconds=3600, min_tokens=100))
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is None
        assert cache_cfg is not None
        assert cache_cfg.cache_intervals == 20
        assert cache_cfg.ttl_seconds == 3600
        assert cache_cfg.min_tokens == 100

    def test_cache_defaults(self):
        ctx_config = ContextConfig(cache=ContextCacheSettings())
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is None
        assert cache_cfg is not None
        assert cache_cfg.cache_intervals == 10
        assert cache_cfg.ttl_seconds == 1800
        assert cache_cfg.min_tokens == 0

    def test_compaction_with_summarizer(self):
        ctx_config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                summarizer_model=OpenAI(type="openai", model="gpt-4o-mini"),
                prompt_template="Summarize: {{events}}",
            )
        )
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is not None
        assert events_cfg.summarizer is not None
        assert cache_cfg is None

    def test_compaction_no_summarizer_without_model(self):
        ctx_config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                prompt_template="Summarize these events",
            )
        )
        events_cfg, _ = build_adk_context_configs(ctx_config)
        assert events_cfg is not None
        assert events_cfg.summarizer is None

    def test_both_configs(self):
        ctx_config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=10,
                overlap_size=3,
                token_threshold=1000,
                event_retention_size=5,
            ),
            cache=ContextCacheSettings(cache_intervals=15),
        )
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is not None
        assert events_cfg.compaction_interval == 10
        assert events_cfg.overlap_size == 3
        assert cache_cfg is not None
        assert cache_cfg.cache_intervals == 15


class TestJsonRoundTrip:
    def test_full_config_round_trip(self):
        go_style_json = {
            "model": {"type": "openai", "model": "gpt-4"},
            "description": "My agent",
            "instruction": "Help the user",
            "http_tools": None,
            "sse_tools": None,
            "remote_agents": None,
            "execute_code": False,
            "stream": False,
            "context_config": {
                "compaction": {
                    "compaction_interval": 10,
                    "overlap_size": 3,
                    "summarizer_model": {"type": "openai", "model": "gpt-4o-mini"},
                    "prompt_template": "Summarize: {{events}}",
                    "token_threshold": 500,
                    "event_retention_size": 5,
                },
                "cache": {
                    "cache_intervals": 20,
                    "ttl_seconds": 3600,
                    "min_tokens": 100,
                },
            },
        }
        config = AgentConfig.model_validate(go_style_json)
        assert config.context_config is not None
        assert config.context_config.compaction.compaction_interval == 10
        assert isinstance(config.context_config.compaction.summarizer_model, OpenAI)
        assert config.context_config.compaction.summarizer_model.model == "gpt-4o-mini"
        assert config.context_config.cache.cache_intervals == 20
        assert config.context_config.cache.ttl_seconds == 3600

    def test_config_without_context_round_trip(self):
        go_style_json = {
            "model": {"type": "openai", "model": "gpt-4"},
            "description": "My agent",
            "instruction": "Help the user",
            "http_tools": [],
            "sse_tools": [],
            "remote_agents": [],
            "execute_code": False,
            "stream": True,
        }
        config = AgentConfig.model_validate(go_style_json)
        assert config.context_config is None
        assert config.stream is True
