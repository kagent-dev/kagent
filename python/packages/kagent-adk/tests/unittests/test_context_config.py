import json

import pytest
from pydantic import ValidationError

from kagent.adk.types import (
    AgentConfig,
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

    def test_compaction_only(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {"compaction": {"compaction_interval": 5, "overlap_size": 2}}
        config = AgentConfig.model_validate(data)
        assert config.context_config is not None
        assert config.context_config.compaction is not None
        assert config.context_config.compaction.compaction_interval == 5
        assert config.context_config.compaction.overlap_size == 2

    def test_compaction_with_all_fields(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 10,
                "overlap_size": 3,
                "token_threshold": 1000,
                "event_retention_size": 5,
            },
        }
        config = AgentConfig.model_validate(data)
        assert config.context_config.compaction.compaction_interval == 10
        assert config.context_config.compaction.overlap_size == 3
        assert config.context_config.compaction.token_threshold == 1000
        assert config.context_config.compaction.event_retention_size == 5

    def test_compaction_with_summarizer_model(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model": {"type": "openai", "model": "gpt-4o-mini"},
                "prompt_template": "Summarize: {{events}}",
            }
        }
        config = AgentConfig.model_validate(data)
        comp = config.context_config.compaction
        assert comp.summarizer_model is not None
        assert isinstance(comp.summarizer_model, OpenAI)
        assert comp.summarizer_model.model == "gpt-4o-mini"
        assert comp.prompt_template == "Summarize: {{events}}"

    def test_compaction_with_gemini_summarizer(self):
        data = json.loads(_make_agent_config_json())
        data["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model": {"type": "gemini", "model": "gemini-1.5-flash"},
            }
        }
        config = AgentConfig.model_validate(data)
        assert isinstance(config.context_config.compaction.summarizer_model, Gemini)

    def test_compaction_missing_required_fields(self):
        with pytest.raises(ValidationError):
            ContextCompressionSettings(compaction_interval=5)  # missing overlap_size

    def test_round_trip_serialization(self):
        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                token_threshold=1000,
            ),
        )
        json_str = config.model_dump_json()
        parsed = ContextConfig.model_validate_json(json_str)
        assert parsed.compaction.compaction_interval == 5
        assert parsed.compaction.overlap_size == 2
        assert parsed.compaction.token_threshold == 1000


class TestBuildAdkContextConfigs:
    def test_compaction_only(self):
        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
            )
        )
        events_cfg, cache_cfg = build_adk_context_configs(config)
        assert events_cfg is not None
        assert events_cfg.compaction_interval == 5
        assert events_cfg.overlap_size == 2
        assert events_cfg.summarizer is None
        assert cache_cfg is None

    def test_compaction_with_summarizer_model(self):
        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                summarizer_model=OpenAI(type="openai", model="gpt-4o-mini"),
                prompt_template="Summarize: {{events}}",
            )
        )
        events_cfg, _ = build_adk_context_configs(config)
        assert events_cfg is not None
        assert events_cfg.summarizer is not None

    def test_empty_config(self):
        config = ContextConfig()
        events_cfg, cache_cfg = build_adk_context_configs(config)
        assert events_cfg is None
        assert cache_cfg is None
