"""The controller and Python ADK share the generated protobuf contract."""

import base64
import json
from pathlib import Path

import pytest
from google.adk.tools.mcp_tool import SseConnectionParams, StreamableHTTPConnectionParams
from google.protobuf.json_format import MessageToJson, ParseError
from protovalidate import ValidationError

from kagent.adk.types import _create_llm_from_model_config, _mcp_params, parse_config


def test_shared_go_agent_contract():
    path = Path(__file__).resolve().parents[5] / "go/adk/pkg/config/testdata/agent.json"
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
    assert json.loads(bedrock.additional_model_request_fields) == {
        "nested": {"enabled": False},
        "values": [0, None, "text"],
        "thinking": {"budget_tokens": 1024},
        "large": 9007199254740993,
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


_NUMERIC_CASES = json.loads(
    (Path(__file__).resolve().parents[5] / "go/adk/pkg/config/testdata/numeric_cases.json").read_text()
)


@pytest.mark.parametrize("case", _NUMERIC_CASES, ids=lambda case: case["name"])
def test_shared_numeric_validation(case):
    if case["valid"]:
        parse_config(case["config"])
    else:
        with pytest.raises((ValidationError, ParseError)):
            parse_config(case["config"])


def test_fractional_mcp_timeout():
    config = parse_config(
        {
            "agent": {
                "http_tools": [
                    {"params": {"url": "https://mcp.example.com", "timeout": "0.250s", "sse_read_timeout": "0.750s"}}
                ]
            }
        }
    )
    params = _mcp_params(config.agent.http_tools[0].params, StreamableHTTPConnectionParams)
    assert params.timeout == 0.25
    assert params.sse_read_timeout == 0.75


@pytest.mark.parametrize("raw", [None, {}, {"thinking": {"budget_tokens": 1024}, "large": 9007199254740993}])
def test_bedrock_provider_json(raw):
    settings = (
        {} if raw is None else {"additional_model_request_fields": base64.b64encode(json.dumps(raw).encode()).decode()}
    )
    config = parse_config({"agent": {"model": {"model": "claude", "bedrock": settings}}})
    llm = _create_llm_from_model_config(config.agent.model)
    assert llm.additional_model_request_fields == raw
    if raw:
        assert type(llm.additional_model_request_fields["thinking"]["budget_tokens"]) is int
        assert type(llm.additional_model_request_fields["large"]) is int


@pytest.mark.parametrize("raw", [b"null", b"[]", b"1", b"{", b"{} {}"])
def test_bedrock_invalid_provider_json(raw):
    config = parse_config(
        {
            "agent": {
                "model": {
                    "model": "claude",
                    "bedrock": {"additional_model_request_fields": base64.b64encode(raw).decode()},
                }
            }
        }
    )
    with pytest.raises(ValueError):
        _create_llm_from_model_config(config.agent.model)


@pytest.mark.parametrize(
    "field,params_type", [("http_tools", StreamableHTTPConnectionParams), ("sse_tools", SseConnectionParams)]
)
def test_mcp_explicit_options_and_defaults(field, params_type):
    values = {"url": "https://mcp.example.com", "headers": {"X-Test": "value"}}
    config = parse_config({"agent": {field: [{"params": values}]}})
    params = _mcp_params(getattr(config.agent, field)[0].params, params_type)
    defaults = params_type(url=values["url"])
    assert params.headers == values["headers"]
    assert params.timeout == defaults.timeout
    assert params.sse_read_timeout == defaults.sse_read_timeout
    if field == "http_tools":
        config.agent.http_tools[0].params.terminate_on_close = False
        assert _mcp_params(config.agent.http_tools[0].params, params_type).terminate_on_close is False


@pytest.mark.parametrize(
    "provider,field",
    [
        ("openai", "timeout"),
        ("mistral", "timeout"),
        ("anthropic", "timeout"),
        ("bedrock", "read_timeout"),
        ("bedrock", "connect_timeout"),
    ],
)
def test_fractional_model_timeout(provider, field):
    config = parse_config({"agent": {"model": {"model": "test-model", provider: {field: "0.250s"}}}})
    model = _create_llm_from_model_config(config.agent.model)
    assert getattr(model, field) == 0.25
