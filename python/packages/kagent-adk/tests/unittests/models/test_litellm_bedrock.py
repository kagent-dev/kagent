"""Unit tests for Bedrock tool-name sanitization in KAgentLiteLlm."""
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai.types import Content, FunctionCall, Part

from kagent.adk.models._litellm import (
    _sanitize_llm_request,
    _sanitize_llm_response,
    _sanitize_tool_name,
)


# ---------------------------------------------------------------------------
# _sanitize_tool_name
# ---------------------------------------------------------------------------


def test_sanitize_tool_name_valid_unchanged():
    assert _sanitize_tool_name("valid_tool-name1", 0) == "valid_tool-name1"


def test_sanitize_tool_name_replaces_dots():
    result = _sanitize_tool_name("tool.name", 0)
    assert result == "tool_name"


def test_sanitize_tool_name_replaces_spaces():
    result = _sanitize_tool_name("my tool", 0)
    assert result == "my_tool"


def test_sanitize_tool_name_empty_uses_fallback():
    result = _sanitize_tool_name("", 3)
    assert result == "unknown_tool_3"


def test_sanitize_tool_name_composite_suffix():
    result = _sanitize_tool_name("", "2_5")
    assert result == "unknown_tool_2_5"


def test_sanitize_tool_name_logs_at_debug(caplog):
    import logging

    with caplog.at_level(logging.DEBUG, logger="kagent.adk.models._litellm"):
        _sanitize_tool_name("bad.name", 0)
    assert any("Sanitized invalid Bedrock tool name" in r.message for r in caplog.records)
    assert all(r.levelname == "DEBUG" for r in caplog.records if "Sanitized" in r.message)


# ---------------------------------------------------------------------------
# _sanitize_llm_request
# ---------------------------------------------------------------------------


def _make_request_with_function_call(name: str) -> LlmRequest:
    fc = FunctionCall(name=name, args={})
    part = Part(function_call=fc)
    content = Content(parts=[part], role="model")
    req = LlmRequest()
    req.contents = [content]
    return req


def test_sanitize_llm_request_fixes_invalid_name():
    req = _make_request_with_function_call("bad.tool.name")
    _sanitize_llm_request(req)
    assert req.contents[0].parts[0].function_call.name == "bad_tool_name"


def test_sanitize_llm_request_leaves_valid_name():
    req = _make_request_with_function_call("good_tool")
    _sanitize_llm_request(req)
    assert req.contents[0].parts[0].function_call.name == "good_tool"


def test_sanitize_llm_request_no_parts_no_error():
    content = Content(parts=[], role="model")
    req = LlmRequest()
    req.contents = [content]
    _sanitize_llm_request(req)  # should not raise


# ---------------------------------------------------------------------------
# _sanitize_llm_response
# ---------------------------------------------------------------------------


def _make_response_with_function_call(name: str) -> LlmResponse:
    fc = FunctionCall(name=name, args={})
    part = Part(function_call=fc)
    content = Content(parts=[part], role="model")
    resp = LlmResponse()
    resp.content = content
    return resp


def test_sanitize_llm_response_fixes_invalid_name():
    resp = _make_response_with_function_call("bad.tool")
    result = _sanitize_llm_response(resp, 0)
    assert result.content.parts[0].function_call.name == "bad_tool"


def test_sanitize_llm_response_leaves_valid_name():
    resp = _make_response_with_function_call("valid_tool")
    result = _sanitize_llm_response(resp, 0)
    assert result.content.parts[0].function_call.name == "valid_tool"


def test_sanitize_llm_response_no_collision_across_parts():
    fc0 = FunctionCall(name="", args={})
    fc1 = FunctionCall(name="", args={})
    content = Content(parts=[Part(function_call=fc0), Part(function_call=fc1)], role="model")
    resp = LlmResponse()
    resp.content = content
    _sanitize_llm_response(resp, 1)
    names = [p.function_call.name for p in resp.content.parts]
    # Each fallback name must be unique (composite idx_i suffix)
    assert names[0] == "unknown_tool_1_0"
    assert names[1] == "unknown_tool_1_1"


def test_sanitize_llm_response_no_content_no_error():
    resp = LlmResponse()
    resp.content = None
    _sanitize_llm_response(resp, 0)  # should not raise


# ---------------------------------------------------------------------------
# KAgentLiteLlm.generate_content_async — Bedrock vs non-Bedrock routing
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_generate_content_async_bedrock_sanitizes(monkeypatch):
    from kagent.adk.models._litellm import KAgentLiteLlm

    model = KAgentLiteLlm(model="bedrock/anthropic.claude-3-sonnet")

    req = _make_request_with_function_call("bad.name")
    req.model = "bedrock/anthropic.claude-3-sonnet"

    resp = _make_response_with_function_call("bad.response.name")

    async def fake_super(*args, **kwargs):
        yield resp

    with patch.object(
        KAgentLiteLlm.__bases__[0], "generate_content_async", return_value=fake_super()
    ):
        results = []
        async for r in model.generate_content_async(req):
            results.append(r)

    assert req.contents[0].parts[0].function_call.name == "bad_name"
    assert results[0].content.parts[0].function_call.name == "bad_response_name"


@pytest.mark.asyncio
async def test_generate_content_async_non_bedrock_no_sanitization(monkeypatch):
    from kagent.adk.models._litellm import KAgentLiteLlm

    model = KAgentLiteLlm(model="openai/gpt-4o")

    req = _make_request_with_function_call("bad.name")
    req.model = "openai/gpt-4o"

    resp = _make_response_with_function_call("bad.name")

    async def fake_super(*args, **kwargs):
        yield resp

    with patch.object(
        KAgentLiteLlm.__bases__[0], "generate_content_async", return_value=fake_super()
    ):
        results = []
        async for r in model.generate_content_async(req):
            results.append(r)

    # name must remain unchanged for non-Bedrock models
    assert req.contents[0].parts[0].function_call.name == "bad.name"
    assert results[0].content.parts[0].function_call.name == "bad.name"
