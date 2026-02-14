"""Tests for context management configuration types and builder."""

import json
from unittest.mock import MagicMock, patch

import pytest
from pydantic import ValidationError

from kagent.adk.types import (
    AgentConfig,
    ContextCacheSettings,
    ContextCompressionSettings,
    ContextConfig,
    build_adk_context_configs,
)


def _make_agent_config_json(**context_kwargs) -> str:
    """Helper to create AgentConfig JSON with optional context config."""
    config = {
        "model": {"type": "openai", "model": "gpt-4"},
        "description": "test agent",
        "instruction": "test instruction",
    }
    if context_kwargs:
        config["context_config"] = context_kwargs
    return json.dumps(config)


class TestContextConfigParsing:
    """Tests for parsing context config from JSON."""

    def test_no_context_config(self):
        """AgentConfig without context_config should have None."""
        config = AgentConfig.model_validate_json(_make_agent_config_json())
        assert config.context_config is None

    def test_empty_context_config(self):
        """AgentConfig with empty context_config should have empty ContextConfig."""
        json_str = _make_agent_config_json()
        data = json.loads(json_str)
        data["context_config"] = {}
        config = AgentConfig.model_validate(data)
        assert config.context_config is not None
        assert config.context_config.compaction is None
        assert config.context_config.cache is None

    def test_compaction_only(self):
        """Parse compaction config without cache."""
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {"compaction": {"compaction_interval": 5, "overlap_size": 2}}
        config = AgentConfig.model_validate(data)
        assert config.context_config is not None
        assert config.context_config.compaction is not None
        assert config.context_config.compaction.compaction_interval == 5
        assert config.context_config.compaction.overlap_size == 2
        assert config.context_config.cache is None

    def test_cache_only(self):
        """Parse cache config without compaction."""
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
        """Parse both compaction and cache."""
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

    def test_compaction_with_summarizer(self):
        """Parse compaction with summarizer fields."""
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model_name": "gpt-4o-mini",
                "prompt_template": "Summarize these events: {{events}}",
            }
        }
        config = AgentConfig.model_validate(data)
        comp = config.context_config.compaction
        assert comp.summarizer_model_name == "gpt-4o-mini"
        assert comp.prompt_template == "Summarize these events: {{events}}"

    def test_compaction_missing_required_fields(self):
        """Compaction missing required fields should fail validation."""
        with pytest.raises(ValidationError):
            ContextCompressionSettings(compaction_interval=5)  # missing overlap_size

        with pytest.raises(ValidationError):
            ContextCompressionSettings(overlap_size=2)  # missing compaction_interval

    def test_cache_with_defaults(self):
        """Cache with empty/partial config should accept all None."""
        cache = ContextCacheSettings()
        assert cache.cache_intervals is None
        assert cache.ttl_seconds is None
        assert cache.min_tokens is None

    def test_null_vs_absent_context_config(self):
        """Both null and absent context_config should result in None."""
        # Absent
        data = json.loads(_make_agent_config_json())
        config1 = AgentConfig.model_validate(data)
        assert config1.context_config is None

        # Explicit null
        data["context_config"] = None
        config2 = AgentConfig.model_validate(data)
        assert config2.context_config is None


class TestBuildAdkContextConfigs:
    """Tests for build_adk_context_configs function."""

    def test_compaction_basic(self):
        """Build EventsCompactionConfig from basic compaction settings."""
        ctx_config = ContextConfig(compaction=ContextCompressionSettings(compaction_interval=5, overlap_size=2))
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is not None
        assert events_cfg.compaction_interval == 5
        assert events_cfg.overlap_size == 2
        assert events_cfg.summarizer is None
        assert cache_cfg is None

    def test_cache_basic(self):
        """Build ContextCacheConfig from basic cache settings."""
        ctx_config = ContextConfig(cache=ContextCacheSettings(cache_intervals=20, ttl_seconds=3600, min_tokens=100))
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is None
        assert cache_cfg is not None
        assert cache_cfg.cache_intervals == 20
        assert cache_cfg.ttl_seconds == 3600
        assert cache_cfg.min_tokens == 100

    def test_cache_defaults(self):
        """Build ContextCacheConfig with empty settings uses ADK defaults."""
        ctx_config = ContextConfig(cache=ContextCacheSettings())
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config)
        assert events_cfg is None
        assert cache_cfg is not None
        # ADK defaults: cache_intervals=10, ttl_seconds=1800, min_tokens=0
        assert cache_cfg.cache_intervals == 10
        assert cache_cfg.ttl_seconds == 1800
        assert cache_cfg.min_tokens == 0

    def test_compaction_with_summarizer(self):
        """Build compaction config with LLM summarizer."""
        ctx_config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                summarizer_model_name="gpt-4o-mini",
                prompt_template="Summarize: {{events}}",
            )
        )
        events_cfg, cache_cfg = build_adk_context_configs(ctx_config, agent_model_name="gpt-4")
        assert events_cfg is not None
        assert events_cfg.summarizer is not None
        assert cache_cfg is None

    def test_compaction_summarizer_falls_back_to_agent_model(self):
        """Summarizer without explicit model uses agent's model."""
        ctx_config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                prompt_template="Summarize these events",
            )
        )
        events_cfg, _ = build_adk_context_configs(ctx_config, agent_model_name="gpt-4")
        assert events_cfg is not None
        assert events_cfg.summarizer is not None

    def test_both_configs(self):
        """Build both compaction and cache configs."""
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
    """Test that Go-serialized config.json can be parsed by Python."""

    def test_full_config_round_trip(self):
        """Parse a config.json matching Go's output format."""
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
                    "summarizer_model_name": "gpt-4o-mini",
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
        assert config.context_config.compaction.summarizer_model_name == "gpt-4o-mini"
        assert config.context_config.cache.cache_intervals == 20
        assert config.context_config.cache.ttl_seconds == 3600

    def test_config_without_context_round_trip(self):
        """Existing config without context_config still parses correctly."""
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
