"""Regression test for kagent#1797: the controller has historically emitted
``http_tools[*].tools = null`` for RemoteMCPServer refs without an explicit
``toolNames`` filter. The runtime must tolerate that input rather than crashing
with a pydantic ``list_type`` validation error at startup.
"""

import json

import pytest

from kagent.adk.types import AgentConfig, HttpMcpServerConfig, SseMcpServerConfig


_HTTP_PARAMS = {"url": "http://example/mcp", "headers": {}, "terminate_on_close": True}
_SSE_PARAMS = {"url": "http://example/mcp", "headers": {}}


@pytest.mark.parametrize(
    "model, params",
    [
        (HttpMcpServerConfig, _HTTP_PARAMS),
        (SseMcpServerConfig, _SSE_PARAMS),
    ],
)
def test_tools_none_is_coerced_to_empty_list(model, params):
    cfg = model.model_validate({"params": params, "tools": None})
    assert cfg.tools == []


@pytest.mark.parametrize(
    "model, params",
    [
        (HttpMcpServerConfig, _HTTP_PARAMS),
        (SseMcpServerConfig, _SSE_PARAMS),
    ],
)
def test_tools_missing_defaults_to_empty_list(model, params):
    cfg = model.model_validate({"params": params})
    assert cfg.tools == []


@pytest.mark.parametrize(
    "model, params",
    [
        (HttpMcpServerConfig, _HTTP_PARAMS),
        (SseMcpServerConfig, _SSE_PARAMS),
    ],
)
def test_explicit_tools_list_is_preserved(model, params):
    cfg = model.model_validate({"params": params, "tools": ["alpha", "beta"]})
    assert cfg.tools == ["alpha", "beta"]


def _make_agent_config_blob(http_tools_value, sse_tools_value):
    """Reproduce the exact wire format the kagent controller writes to the
    agent's ``config.json`` Secret. Any field-level coercion in
    ``HttpMcpServerConfig`` / ``SseMcpServerConfig`` must keep this top-level
    construction path working — that path is what crashes the runtime pod on
    startup."""
    return {
        "model": {
            "type": "openai",
            "model": "gpt-4o-mini",
            "api_key": "dummy",
        },
        "description": "repro for #1797",
        "instruction": "test",
        "http_tools": [
            {
                "params": {
                    "url": "http://example/mcp",
                    "headers": {},
                    "terminate_on_close": True,
                },
                "tools": http_tools_value,
            }
        ],
        "sse_tools": [
            {
                "params": {"url": "http://example/sse", "headers": {}},
                "tools": sse_tools_value,
            }
        ],
    }


def test_agent_config_load_with_null_tools_does_not_crash():
    """End-to-end runtime startup path: load the exact JSON blob the controller
    writes when ``toolNames`` is omitted. Before the fix this raised
    ``ValidationError: Input should be a valid list ... input_value=None``."""
    blob = _make_agent_config_blob(http_tools_value=None, sse_tools_value=None)
    cfg = AgentConfig.model_validate_json(json.dumps(blob))
    assert cfg.http_tools[0].tools == []
    assert cfg.sse_tools[0].tools == []


def test_agent_config_load_with_explicit_tools_preserved():
    blob = _make_agent_config_blob(
        http_tools_value=["alpha", "beta"],
        sse_tools_value=["gamma"],
    )
    cfg = AgentConfig.model_validate_json(json.dumps(blob))
    assert cfg.http_tools[0].tools == ["alpha", "beta"]
    assert cfg.sse_tools[0].tools == ["gamma"]
