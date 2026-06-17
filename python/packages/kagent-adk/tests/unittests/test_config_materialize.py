import os

import pytest

from kagent.adk._config_materialize import materialize_from_env


def test_materializes_present_env_vars(tmp_path, monkeypatch):
    monkeypatch.setenv("KAGENT_CONFIG_JSON", '{"model": {"type": "openai"}}')
    monkeypatch.setenv("KAGENT_AGENT_CARD_JSON", '{"name": "test"}')
    monkeypatch.setenv("KAGENT_SRT_SETTINGS_JSON", '{"network": {}}')
    monkeypatch.delenv("KAGENT_TOKEN", raising=False)

    config_dir = tmp_path / "config"
    materialize_from_env(str(config_dir))

    assert (config_dir / "config.json").read_text() == '{"model": {"type": "openai"}}'
    assert (config_dir / "agent-card.json").read_text() == '{"name": "test"}'
    assert (config_dir / "srt-settings.json").read_text() == '{"network": {}}'
    # Written with 0600 permissions, matching the Go ADK.
    assert oct(os.stat(config_dir / "config.json").st_mode & 0o777) == "0o600"


def test_noop_when_env_absent(tmp_path, monkeypatch):
    for key in ("KAGENT_CONFIG_JSON", "KAGENT_AGENT_CARD_JSON", "KAGENT_SRT_SETTINGS_JSON", "KAGENT_TOKEN"):
        monkeypatch.delenv(key, raising=False)

    config_dir = tmp_path / "config"
    # Should not raise and should not create the directory/files.
    materialize_from_env(str(config_dir))

    assert not (config_dir / "config.json").exists()


def test_blank_env_is_skipped(tmp_path, monkeypatch):
    monkeypatch.setenv("KAGENT_CONFIG_JSON", "   ")
    monkeypatch.delenv("KAGENT_AGENT_CARD_JSON", raising=False)

    config_dir = tmp_path / "config"
    materialize_from_env(str(config_dir))

    assert not (config_dir / "config.json").exists()


def test_partial_env_only_writes_present(tmp_path, monkeypatch):
    monkeypatch.setenv("KAGENT_CONFIG_JSON", "{}")
    monkeypatch.delenv("KAGENT_AGENT_CARD_JSON", raising=False)
    monkeypatch.delenv("KAGENT_SRT_SETTINGS_JSON", raising=False)

    config_dir = tmp_path / "config"
    materialize_from_env(str(config_dir))

    assert (config_dir / "config.json").exists()
    assert not (config_dir / "agent-card.json").exists()
    assert not (config_dir / "srt-settings.json").exists()
