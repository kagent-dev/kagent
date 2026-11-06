"""Tests for converting LangGraph events into A2A artifact events."""

from kagent.core.a2a import A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL, A2A_PART_TYPE_METADATA_KEY
from langchain_core.messages import AIMessage, ToolMessage

from kagent.langgraph._converters import _convert_langgraph_event_to_a2a


async def _convert(*messages) -> list:
    return await _convert_langgraph_event_to_a2a(
        {"agent": {"messages": list(messages)}}, task_id="task-1", context_id="context-1", sent_message_ids=set()
    )


def _texts(events) -> list[str]:
    return [part.text for event in events for part in event.artifact.parts if part.HasField("text")]


async def test_string_content_is_published_as_text():
    events = await _convert(AIMessage(content="def add(a, b): ..."))
    assert _texts(events) == ["def add(a, b): ..."]


async def test_content_blocks_are_published_as_text():
    """Gemini answers with a list of blocks after a tool call: text plus a thought signature."""
    answer = AIMessage(
        content=[
            {"type": "text", "text": "def add(a: int, b: int) -> int:\n"},
            {"type": "text", "text": "    return a + b"},
            {"type": "thinking", "thinking": "internal", "signature": "abc"},
        ]
    )
    events = await _convert(answer)
    assert _texts(events) == ["def add(a: int, b: int) -> int:\n    return a + b"]


async def test_bare_strings_in_a_content_list_are_text():
    events = await _convert(AIMessage(content=["first ", {"type": "text", "text": "second"}]))
    assert _texts(events) == ["first second"]


async def test_blocks_without_text_publish_nothing():
    events = await _convert(AIMessage(content=[{"type": "thinking", "thinking": "internal", "signature": "abc"}]))
    assert events == []


async def test_tool_calls_with_empty_content_keep_only_the_call():
    call = AIMessage(content="", tool_calls=[{"id": "call-1", "name": "check_syntax", "args": {"code": "x"}}])
    (event,) = await _convert(call)
    (part,) = event.artifact.parts
    assert not part.HasField("text")
    assert part.metadata[A2A_PART_TYPE_METADATA_KEY] == A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL


async def test_the_answer_after_a_tool_call_is_not_lost():
    """The sequence that used to end with only the tool result as an artifact."""
    events = await _convert(
        AIMessage(content="", tool_calls=[{"id": "call-1", "name": "check_syntax", "args": {"code": "x"}}]),
        ToolMessage(content='{"valid": true}', tool_call_id="call-1", name="check_syntax"),
        AIMessage(content=[{"type": "text", "text": "def add(a, b): ..."}, {"type": "text", "text": "", "extras": {}}]),
    )
    assert len(events) == 3
    assert _texts(events) == ["def add(a, b): ..."]
