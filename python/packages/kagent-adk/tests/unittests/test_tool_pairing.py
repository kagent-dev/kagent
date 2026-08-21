"""Tests for tool call/response pairing repair."""

import pytest
from google.adk.events.event import Event
from google.adk.models.anthropic_llm import content_to_message_param
from google.adk.models.llm_request import LlmRequest
from google.genai import types

from kagent.adk._tool_pairing import (
    MISSING_TOOL_RESULT,
    PENDING_TOOL_RESULT,
    repair_tool_call_pairing_callback,
)


class _Session:
    def __init__(self, events):
        self.events = events


class _Context:
    """Stands in for ADK's CallbackContext, which exposes `session`."""

    def __init__(self, events=None):
        self.session = _Session(events or [])


def _call(call_id: str, name: str = "get_pods") -> types.Content:
    return types.Content(
        role="model",
        parts=[types.Part(function_call=types.FunctionCall(id=call_id, name=name, args={"ns": "default"}))],
    )


def _calls(*ids: str) -> types.Content:
    return types.Content(
        role="model",
        parts=[types.Part(function_call=types.FunctionCall(id=i, name="get_pods", args={})) for i in ids],
    )


def _response(call_id: str, name: str = "get_pods", text: str = "pod X running") -> types.Content:
    return types.Content(
        role="user",
        parts=[types.Part(function_response=types.FunctionResponse(id=call_id, name=name, response={"result": text}))],
    )


def _text(role: str, text: str) -> types.Content:
    return types.Content(role=role, parts=[types.Part(text=text)])


def _pending_event(*call_ids: str) -> Event:
    """An event whose tool calls ADK is holding open (approval, ask_user, long-running)."""
    return Event(
        author="agent",
        invocation_id="i",
        content=types.Content(
            role="model",
            parts=[types.Part(function_call=types.FunctionCall(id=i, name="approve", args={})) for i in call_ids],
        ),
        long_running_tool_ids=set(call_ids),
    )


def _repair(contents: list[types.Content], context: _Context | None = None) -> list[types.Content]:
    request = LlmRequest(contents=contents)
    repair_tool_call_pairing_callback(callback_context=context or _Context(), llm_request=request)
    return request.contents


def _responses_in(content: types.Content) -> list[types.FunctionResponse]:
    return [p.function_response for p in content.parts or [] if p.function_response]


class TestSynthesizeMissingResponses:
    def test_dangling_call_at_end_of_history(self):
        """The interrupted-turn case from the issue: nothing follows the call."""
        contents = _repair([_text("user", "what's failing?"), _call("abc")])

        assert len(contents) == 3
        responses = _responses_in(contents[2])
        assert len(responses) == 1
        assert responses[0].id == "abc"
        assert responses[0].name == "get_pods"
        assert responses[0].response == {"result": MISSING_TOOL_RESULT}
        assert contents[2].role == "user"

    def test_result_inserted_before_a_following_user_message(self):
        """A second message reached the history before the result did."""
        contents = _repair([_call("abc"), _text("user", "second message")])

        assert len(contents) == 3
        assert _responses_in(contents[1])[0].id == "abc"
        assert contents[2].parts[0].text == "second message"

    def test_sibling_response_turn_is_reused(self):
        """A partially answered call turn gets its gap filled, not a new turn."""
        contents = _repair([_calls("abc", "def"), _response("abc")])

        assert len(contents) == 2
        responses = _responses_in(contents[1])
        assert [r.id for r in responses] == ["abc", "def"]
        assert responses[0].response == {"result": "pod X running"}
        assert responses[1].response == {"result": MISSING_TOOL_RESULT}

    def test_only_the_unanswered_call_is_synthesized(self):
        contents = _repair([_calls("abc", "def"), _response("def", text="log line")])

        responses = _responses_in(contents[1])
        assert {r.id for r in responses} == {"abc", "def"}
        by_id = {r.id: r.response for r in responses}
        assert by_id["def"] == {"result": "log line"}
        assert by_id["abc"] == {"result": MISSING_TOOL_RESULT}

    def test_non_adjacent_response_is_still_repaired(self):
        """Positional pairing: a response elsewhere does not satisfy the provider."""
        contents = _repair([_call("abc"), _text("user", "are you there?"), _response("abc")])

        assert _responses_in(contents[1])[0].response == {"result": MISSING_TOOL_RESULT}

    def test_call_without_id_still_gets_a_response(self):
        contents = _repair(
            [
                types.Content(
                    role="model", parts=[types.Part(function_call=types.FunctionCall(name="get_pods", args={}))]
                )
            ]
        )

        assert len(contents) == 2
        assert _responses_in(contents[1])[0].id is None

    def test_two_calls_without_ids_each_get_their_own_response(self):
        """One response must not answer every id-less call in the turn.

        Gemini omits call ids, and ADK strips its own `adk-` ids before the
        request is built, so id-less siblings are not hypothetical.
        """
        call_turn = types.Content(
            role="model",
            parts=[
                types.Part(function_call=types.FunctionCall(name="get_pods", args={})),
                types.Part(function_call=types.FunctionCall(name="get_logs", args={})),
            ],
        )
        answered = types.Content(
            role="user",
            parts=[types.Part(function_response=types.FunctionResponse(name="get_pods", response={"result": "ok"}))],
        )

        contents = _repair([call_turn, answered])

        responses = _responses_in(contents[1])
        assert len(responses) == 2
        assert [r.response for r in responses] == [{"result": "ok"}, {"result": MISSING_TOOL_RESULT}]


class TestPendingLongRunningCalls:
    """Calls ADK holds open on purpose must not be reported as empty results."""

    def test_pending_approval_gets_the_pending_placeholder(self):
        """A plain message sent while an approval is pending reaches this callback.

        `_translate_hitl_response` only rewrites messages carrying a HITL payload,
        so an ordinary chat message during a pending confirmation arrives as a
        normal turn with the approval call still unanswered.
        """
        context = _Context([_pending_event("call-1")])

        contents = _repair([_call("call-1", name="approve"), _text("user", "never mind, what about X?")], context)

        assert _responses_in(contents[1])[0].response == {"result": PENDING_TOOL_RESULT}

    def test_interrupted_call_in_the_same_session_keeps_the_missing_placeholder(self):
        context = _Context([_pending_event("call-1")])

        contents = _repair([_call("other"), _text("user", "still there?")], context)

        assert _responses_in(contents[1])[0].response == {"result": MISSING_TOOL_RESULT}

    def test_missing_context_falls_back_to_the_neutral_placeholder(self):
        request = LlmRequest(contents=[_call("abc")])
        repair_tool_call_pairing_callback(callback_context=None, llm_request=request)

        assert _responses_in(request.contents[1])[0].response == {"result": MISSING_TOOL_RESULT}


class TestDoesNotDisturbHealthyHistory:
    def test_paired_history_is_unchanged(self):
        original = [_text("user", "what's failing?"), _call("abc"), _response("abc"), _text("model", "pod X is down")]
        before = [c.model_dump_json() for c in original]

        contents = _repair(original)

        assert [c.model_dump_json() for c in contents] == before

    def test_history_without_tools_is_unchanged(self):
        original = [_text("user", "hi"), _text("model", "hello")]
        before = [c.model_dump_json() for c in original]

        assert [c.model_dump_json() for c in _repair(original)] == before

    def test_answered_pending_call_is_left_alone(self):
        """Once the human answers, the real response is present and wins."""
        context = _Context([_pending_event("call-1")])
        original = [_call("call-1", name="approve"), _response("call-1", name="approve", text="approved")]
        before = [c.model_dump_json() for c in original]

        assert [c.model_dump_json() for c in _repair(original, context)] == before

    def test_empty_and_missing_parts_do_not_raise(self):
        request = LlmRequest(contents=[])
        repair_tool_call_pairing_callback(callback_context=_Context(), llm_request=request)
        assert request.contents == []

        contents = _repair([types.Content(role="user", parts=None), _call("abc")])
        assert _responses_in(contents[-1])[0].id == "abc"


class TestAnthropicPairingInvariant:
    """The invariant the Anthropic API enforces, over repaired contents."""

    @staticmethod
    def _assert_paired(contents: list[types.Content]) -> None:
        messages = [content_to_message_param(c) for c in contents]
        for index, message in enumerate(messages):
            call_ids = [b["id"] for b in message["content"] if b.get("type") == "tool_use"]
            if not call_ids:
                continue
            assert index + 1 < len(messages), f"message {index} ends the conversation with an unanswered tool_use"
            following = messages[index + 1]
            answered = [b["tool_use_id"] for b in following["content"] if b.get("type") == "tool_result"]
            assert set(call_ids) <= set(answered), f"message {index} has tool_use ids without tool_result: {call_ids}"

    def test_interrupted_turn_produces_a_valid_conversation(self):
        self._assert_paired(_repair([_text("user", "what's failing?"), _call("abc")]))

    def test_message_during_a_running_tool_produces_a_valid_conversation(self):
        self._assert_paired(_repair([_call("abc"), _text("user", "second message")]))

    def test_partially_answered_turn_produces_a_valid_conversation(self):
        self._assert_paired(_repair([_calls("abc", "def"), _response("abc")]))

    def test_pending_approval_produces_a_valid_conversation(self):
        context = _Context([_pending_event("call-1")])
        self._assert_paired(_repair([_call("call-1", name="approve"), _text("user", "what about X?")], context))

    def test_unrepaired_history_would_fail_the_invariant(self):
        """Guards the tests themselves: the invariant must reject the broken input."""
        with pytest.raises(AssertionError):
            self._assert_paired([_text("user", "what's failing?"), _call("abc")])
