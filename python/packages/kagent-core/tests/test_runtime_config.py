import json
from pathlib import Path

import pytest
from google.protobuf.json_format import ParseError
from protovalidate import ValidationError

from kagent.core import KAgentConfig
from kagent.core._runtime_config import RuntimeConfig, parse_runtime_config


def test_shared_go_runtime_contract():
    path = Path(__file__).resolve().parents[4] / "go/adk/pkg/config/testdata/runtime.json"
    cfg = parse_runtime_config(path.read_text())
    assert cfg.max_content_length == 0
    assert cfg.token_propagation.enabled is False
    assert cfg.builtin_tools.bash_venv_path == "/opt/venv"
    assert cfg.builtin_tools.disable_file_search is False
    assert cfg.builtin_tools.disable_bash is True
    assert cfg.telemetry.capture_content is False
    assert cfg.telemetry.traces.timeout.ToMilliseconds() == 1500
    assert KAgentConfig(runtime=cfg).api_url == "http://controller:8083"


def test_runtime_defaults():
    cfg = RuntimeConfig()
    assert not cfg.HasField("max_content_length")
    assert not cfg.builtin_tools.disable_file_search
    assert not cfg.builtin_tools.disable_bash
    assert not cfg.HasField("telemetry")


@pytest.mark.parametrize(
    "value",
    [
        {"port": "0"},
        {"max_content_length": -1},
        {"control_plane_url": "file:///private"},
        {"token_propagation": {"resources": [""]}},
        {"telemetry": {"max_capture_bytes": -1}},
        {"telemetry": {"resource_attributes": {"service.name": "shared"}}},
        {"telemetry": {"resource_attributes": {"service.namespace": "shared"}}},
        {"log_level": "verbose"},
        {"builtin_tools": {"disable_file_search": "false"}},
        {"builtin_tools": {"disable_bash": "true"}},
        {"unknown": True},
    ],
)
def test_invalid_runtime_settings(value):
    with pytest.raises((ValidationError, ParseError)):
        parse_runtime_config(value)


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
