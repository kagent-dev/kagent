"""Tests for MCP App (UI) tool result compaction for the model."""

from google.adk.models.llm_request import LlmRequest
from google.genai import types as genai_types

from kagent.adk._mcp_apps import (
    MCP_APP_RENDERED_NOTICE,
    MCPAppToolNames,
    compact_mcp_app_response,
    make_mcp_app_model_result_callback,
)


def _function_response_content(name: str, response: dict) -> genai_types.Content:
    return genai_types.Content(
        role="user",
        parts=[genai_types.Part(function_response=genai_types.FunctionResponse(name=name, response=response))],
    )


def test_compact_success_replaces_payload_with_notice():
    response = {
        "content": [{"type": "text", "text": "Weather for Chicago: Rain, 36C, 82%."}],
        "structuredContent": {"temperature": 36, "conditions": "Rain", "humidity": 82},
        "_meta": {"ui": {"resourceUri": "ui://server-everything/weather-dashboard"}},
    }
    compacted = compact_mcp_app_response(response)
    assert compacted["content"] == [{"type": "text", "text": MCP_APP_RENDERED_NOTICE}]
    assert "structuredContent" not in compacted
    # _meta is preserved for downstream tooling.
    assert compacted["_meta"] == response["_meta"]


def test_compact_error_keeps_content_drops_structured():
    response = {
        "content": [{"type": "text", "text": "boom"}],
        "structuredContent": {"x": 1},
        "isError": True,
    }
    compacted = compact_mcp_app_response(response)
    assert compacted["content"] == [{"type": "text", "text": "boom"}]
    assert "structuredContent" not in compacted
    assert compacted["isError"] is True


def test_callback_compacts_only_app_tool_responses():
    app_tool_names = MCPAppToolNames()
    app_tool_names.add("show-weather-dashboard")
    callback = make_mcp_app_model_result_callback(app_tool_names)

    weather = {"structuredContent": {"temperature": 36}, "content": [{"type": "text", "text": "36C"}]}
    echo = {"content": [{"type": "text", "text": "hello"}]}
    request = LlmRequest(
        contents=[
            _function_response_content("show-weather-dashboard", weather),
            _function_response_content("echo", echo),
        ]
    )

    callback(callback_context=None, llm_request=request)

    app_resp = request.contents[0].parts[0].function_response.response
    other_resp = request.contents[1].parts[0].function_response.response
    assert app_resp["content"] == [{"type": "text", "text": MCP_APP_RENDERED_NOTICE}]
    assert "structuredContent" not in app_resp
    # Non-app tool results are left untouched.
    assert other_resp == echo


def test_callback_is_noop_when_no_app_tools():
    callback = make_mcp_app_model_result_callback(MCPAppToolNames())
    weather = {"structuredContent": {"temperature": 36}}
    request = LlmRequest(contents=[_function_response_content("show-weather-dashboard", weather)])
    callback(callback_context=None, llm_request=request)
    assert request.contents[0].parts[0].function_response.response == weather
