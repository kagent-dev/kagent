"""Tests for which LangGraph messages the converter skips as already sent."""

from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_PART_TYPE_METADATA_KEY,
)
from langchain_core.messages import AIMessage, ToolMessage

from kagent.langgraph._converters import _convert_langgraph_event_to_a2a


def _call(call_id: str, city: str) -> AIMessage:
    # A chat model that decides to call a tool answers with empty content.
    return AIMessage(content="", tool_calls=[{"id": call_id, "name": "get_weather", "args": {"city": city}}])


def _result(call_id: str, content: str = "sunny") -> ToolMessage:
    return ToolMessage(content=content, tool_call_id=call_id, name="get_weather")


async def _publish(sent: set[str], node: str, *messages) -> list[dict]:
    """Convert one graph update and return the data of each function part it published."""
    events = await _convert_langgraph_event_to_a2a({node: {"messages": list(messages)}}, "task-1", "context-1", sent)
    return [
        {"type": part.metadata[A2A_PART_TYPE_METADATA_KEY], **MessageToDict(part.data)}
        for event in events
        for part in event.artifact.parts
        if part.HasField("data")
    ]


async def test_each_tool_call_with_empty_content_is_published():
    sent: set[str] = set()

    first = await _publish(sent, "agent", _call("call_1", "Paris"))
    second = await _publish(sent, "agent", _call("call_2", "London"))

    assert [(p["type"], p["id"], p["args"]) for p in first] == [
        (A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL, "call_1", {"city": "Paris"})
    ]
    assert [(p["type"], p["id"], p["args"]) for p in second] == [
        (A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL, "call_2", {"city": "London"})
    ]


async def test_tool_results_with_identical_content_are_each_published():
    sent: set[str] = set()

    first = await _publish(sent, "tools", _result("call_1"))
    second = await _publish(sent, "tools", _result("call_2"))

    assert [(p["type"], p["id"]) for p in first] == [(A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE, "call_1")]
    assert [(p["type"], p["id"]) for p in second] == [(A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE, "call_2")]


async def test_a_replayed_message_is_published_once():
    sent: set[str] = set()
    call, result = _call("call_1", "Paris"), _result("call_1")

    assert await _publish(sent, "agent", call)
    assert await _publish(sent, "tools", result)

    # A node that returns the whole history again must not publish it again.
    assert await _publish(sent, "agent", call, result) == []
