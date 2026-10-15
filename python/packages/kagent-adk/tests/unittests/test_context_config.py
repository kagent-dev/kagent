import json

import pytest
from google.protobuf.json_format import MessageToJson, Parse

from kagent.adk.types import (
    AgentConfig,
    ContextCompressionSettings,
    ContextConfig,
    Gemini,
    ModelConfig,
    OpenAI,
    build_adk_context_configs,
    parse_config,
)


def _make_agent_config_json(**context_kwargs) -> str:
    config = {
        "model": {"model": "gpt-4", "openai": {}},
        "description": "test agent",
        "instruction": "test instruction",
    }
    if context_kwargs:
        config["context_config"] = context_kwargs
    return json.dumps({"agent": config})


def _get_prompt_template(summarizer):
    """Use public attribute if available, fall back to private (resilient across google-adk versions)."""
    pub = getattr(summarizer, "prompt_template", None)
    if pub is not None:
        return pub
    return getattr(summarizer, "_prompt_template", None)


class TestContextConfigParsing:
    def test_no_context_config(self):
        config = parse_config(_make_agent_config_json()).agent
        assert not config.HasField("context_config")

    def test_empty_context_config(self):
        json_str = _make_agent_config_json()
        data = json.loads(json_str)
        data["agent"]["context_config"] = {}
        config = parse_config(data).agent
        assert config.HasField("context_config")
        assert not config.context_config.HasField("compaction")

    def test_compaction_only(self):
        data = json.loads(_make_agent_config_json())
        data["agent"]["context_config"] = {"compaction": {"compaction_interval": 5, "overlap_size": 2}}
        config = parse_config(data).agent
        assert config.HasField("context_config")
        assert config.context_config.HasField("compaction")
        assert config.context_config.compaction.compaction_interval == 5
        assert config.context_config.compaction.overlap_size == 2

    def test_compaction_with_all_fields(self):
        data = json.loads(_make_agent_config_json())
        data["agent"]["context_config"] = {
            "compaction": {
                "compaction_interval": 10,
                "overlap_size": 3,
                "token_threshold": 1000,
                "event_retention_size": 5,
            },
        }
        config = parse_config(data).agent
        assert config.context_config.compaction.compaction_interval == 10
        assert config.context_config.compaction.overlap_size == 3
        assert config.context_config.compaction.token_threshold == 1000
        assert config.context_config.compaction.event_retention_size == 5

    def test_compaction_with_summarizer_model(self):
        data = json.loads(_make_agent_config_json())
        data["agent"]["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model": {"model": "gpt-4o-mini", "openai": {}},
                "prompt_template": "Summarize: {{events}}",
            }
        }
        config = parse_config(data).agent
        comp = config.context_config.compaction
        assert comp.HasField("summarizer_model")
        assert comp.summarizer_model.WhichOneof("provider") == "openai"
        assert comp.summarizer_model.model == "gpt-4o-mini"
        assert comp.prompt_template == "Summarize: {{events}}"

    def test_compaction_with_gemini_summarizer(self):
        data = json.loads(_make_agent_config_json())
        data["agent"]["context_config"] = {
            "compaction": {
                "compaction_interval": 5,
                "overlap_size": 2,
                "summarizer_model": {"model": "gemini-1.5-flash", "gemini": {}},
            }
        }
        config = parse_config(data).agent
        assert config.context_config.compaction.summarizer_model.WhichOneof("provider") == "gemini"

    def test_compaction_preserves_absent_overlap(self):
        config = ContextCompressionSettings(compaction_interval=5)
        assert not config.HasField("overlap_size")

    def test_round_trip_serialization(self):
        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                token_threshold=1000,
            ),
        )
        json_str = MessageToJson(config)
        parsed = Parse(json_str, ContextConfig())
        assert parsed.compaction.compaction_interval == 5
        assert parsed.compaction.overlap_size == 2
        assert parsed.compaction.token_threshold == 1000

    def test_network_config(self):
        data = json.loads(_make_agent_config_json())
        data["agent"]["network"] = {"allowed_domains": ["api.example.com", "*.example.org"]}
        config = parse_config(data).agent
        assert config.network is not None
        assert config.network.allowed_domains == ["api.example.com", "*.example.org"]

    def test_model_accepts_legacy_tls_insecure_skip_verify_field(self):
        data = json.loads(_make_agent_config_json())
        data["agent"]["model"]["tls_insecure_skip_verify"] = True

        config = parse_config(data).agent

        assert config.model.WhichOneof("provider") == "openai"
        assert config.model.tls_insecure_skip_verify is True


class TestBuildAdkContextConfigs:
    def test_optional_fields_preserve_sdk_defaults(self):
        config = ContextConfig(compaction=ContextCompressionSettings(compaction_interval=5, overlap_size=0))
        events_cfg, _ = build_adk_context_configs(config)
        assert events_cfg.token_threshold is None
        assert events_cfg.event_retention_size is None

        config.compaction.overlap_size = 0
        config.compaction.token_threshold = 1000
        config.compaction.event_retention_size = 0
        events_cfg, _ = build_adk_context_configs(config)
        assert events_cfg.overlap_size == 0
        assert events_cfg.token_threshold == 1000
        assert events_cfg.event_retention_size == 0

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
                summarizer_model=ModelConfig(model="gpt-4o-mini", openai=OpenAI()),
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

    def test_summarizer_uses_kagent_default_prompt_when_none_provided(self):
        """When no prompt_template is given, the kagent default that preserves
        tool names should be used instead of the ADK default."""
        from kagent.adk.types import _KAGENT_COMPACTION_PROMPT

        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                summarizer_model=ModelConfig(model="gpt-4o-mini", openai=OpenAI()),
            )
        )
        events_cfg, _ = build_adk_context_configs(config)
        assert events_cfg is not None
        assert events_cfg.summarizer is not None
        assert _get_prompt_template(events_cfg.summarizer) == _KAGENT_COMPACTION_PROMPT
        assert "{conversation_history}" in _KAGENT_COMPACTION_PROMPT

    def test_summarizer_respects_custom_prompt_template(self):
        """A user-supplied prompt_template should have tool name warning appended."""
        from kagent.adk.types import _KAGENT_TOOL_NAME_WARNING

        custom = "My custom prompt: {conversation_history}"
        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                summarizer_model=ModelConfig(model="gpt-4o-mini", openai=OpenAI()),
                prompt_template=custom,
            )
        )
        events_cfg, _ = build_adk_context_configs(config)
        assert _get_prompt_template(events_cfg.summarizer) == custom + _KAGENT_TOOL_NAME_WARNING

    def test_summarizer_preserves_empty_string_prompt_template(self):
        """An empty string prompt_template should fall back to the kagent default."""
        from kagent.adk.types import _KAGENT_COMPACTION_PROMPT

        config = ContextConfig(
            compaction=ContextCompressionSettings(
                compaction_interval=5,
                overlap_size=2,
                summarizer_model=ModelConfig(model="gpt-4o-mini", openai=OpenAI()),
                prompt_template="",
            )
        )
        events_cfg, _ = build_adk_context_configs(config)
        assert _get_prompt_template(events_cfg.summarizer) == _KAGENT_COMPACTION_PROMPT
