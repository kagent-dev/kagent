import json
from pathlib import Path

import pytest
from pydantic import ValidationError

from kagent.core import KAgentConfig
from kagent.core._runtime_config import RuntimeConfig


def test_shared_go_runtime_contract():
    path = Path(__file__).resolve().parents[4] / "go/api/adk/testdata/runtime.json"
    cfg = RuntimeConfig.model_validate_json(path.read_text())
    assert cfg.max_content_length == 0
    assert cfg.token_propagation.enabled is False
    assert cfg.telemetry.capture_content is False
    assert cfg.telemetry.exporters.traces.timeout_ms == 1500
    assert KAgentConfig(runtime=cfg).api_url == "http://controller:8083"


def test_runtime_defaults():
    cfg = RuntimeConfig()
    assert cfg.max_content_length is None
    assert not cfg.file_search_tools
    assert cfg.telemetry.exporters is None


@pytest.mark.parametrize(
    "value",
    [
        {"port": "0"},
        {"max_content_length": -1},
        {"control_plane_url": "file:///private"},
        {"token_propagation": {"resources": [""]}},
        {"telemetry": {"agent_name": "partial"}},
        {"telemetry": {"exporters": {"resource_attributes": {"service.name": "shared"}}}},
        {"telemetry": {"exporters": {"resource_attributes": {"service.namespace": "shared"}}}},
        {"log_level": "verbose"},
        {"file_search_tools": "false"},
        {"unknown": True},
    ],
)
def test_invalid_runtime_settings(value):
    with pytest.raises(ValidationError):
        RuntimeConfig.model_validate(value)


def test_load_generated_root_settings(tmp_path, monkeypatch):
    runtime = {
        "control_plane_url": "http://controller:8083",
        "gateway_url": "http://gateway:8083",
        "name": "test",
        "namespace": "team",
    }
    monkeypatch.setenv("KAGENT_CONFIG_JSON", json.dumps({"runtime": runtime}))
    cfg = KAgentConfig.from_file(tmp_path / "config.json")
    assert cfg.api_url == runtime["control_plane_url"]
    assert cfg.app_name == "team__NS__test"
