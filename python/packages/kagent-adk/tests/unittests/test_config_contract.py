"""The controller and Python ADK share the generated protobuf contract."""

from pathlib import Path

import pytest
from google.protobuf.json_format import MessageToDict, MessageToJson, ParseError
from protovalidate import ValidationError

from kagent.adk.types import parse_config


def test_shared_go_agent_contract():
    path = Path(__file__).resolve().parents[5] / "go/api/adk/testdata/agent.json"
    compiled = parse_config(path.read_text())
    config = compiled.agent
    assert config.model.openai.seed == 9007199254740993
    assert config.model.openai.HasField("temperature")
    assert config.model.openai.temperature == 0
    assert config.HasField("stream") and config.stream is False
    assert config.model.HasField("tls_insecure_skip_verify")
    assert config.http_tools[0].params.HasField("terminate_on_close")
    assert config.http_tools[0].params.terminate_on_close is False
    assert config.context_config.compaction.HasField("overlap_size")
    assert config.output.json_schema == b'{"type":"object","properties":{"answer":{"type":"string"}}}'
    for child in config.sub_agents:
        assert child.name == child.model.WhichOneof("provider")
    assert config.agent_plugins.plugins[1].source.s3.version_id == "version"
    bedrock = next(child.model.bedrock for child in config.sub_agents if child.name == "bedrock")
    assert MessageToDict(bedrock.additional_model_request_fields) == {
        "nested": {"enabled": False},
        "values": [0, None, "text"],
    }
    assert parse_config(MessageToJson(compiled)) == compiled


@pytest.mark.parametrize(
    "value",
    [
        {"agent": {"model": {}}},
        {"agent": {"model": {"openai": {}, "anthropic": {}}}},
        {"unknown": True},
        {"model": {"openai": {}}},
        {"agent": {"runtime": {}}},
        {"agent": {"sub_agents": [{"runtime": {}}]}},
        {"agent": {"sub_agents": [{"sub_agents": [{"runtime": {}}]}]}},
        {"runtime": {"max_content_length": -1}},
    ],
)
def test_invalid_contract(value):
    with pytest.raises((ValidationError, ParseError)):
        parse_config(value)
