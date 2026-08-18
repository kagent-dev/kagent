"""Tool call/response pairing repair for the model request.

A tool call and its result are persisted as two separate session events. If a
turn ends between them (process restart, OOM, client disconnect, cancellation,
or a second message arriving while a slow tool is still running), the session
keeps a ``function_call`` with no matching ``function_response``.

History is replayed verbatim on every later turn, and providers that require
strict pairing reject the whole conversation. Anthropic returns
``tool_use ids were found without tool_result blocks immediately after`` and the
session stays unusable until it is deleted.

The repair runs against the model request, not the store: ADK builds the request
from deep copies of the session events (see ``flows/llm_flows/contents.py``), so
the recorded history stays intact for the UI and sessions already broken in the
field heal on their next turn without a migration.

Pairing is checked positionally, against the immediately following content,
because that is the invariant the provider enforces. A response that exists
somewhere else in the history does not satisfy it.

On a conversation whose calls are all answered this is a no-op, and any
conversation it does change is one the provider would have rejected outright.
"""

from __future__ import annotations

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.genai import types

# Stands in for a result that was never recorded. Deliberately states no cause:
# the call may have been interrupted, or may belong to a long-running tool that
# has not returned yet. Matches the placeholder the OpenAI and Go converters
# already use, so providers that repair on their own keep the same behaviour.
MISSING_TOOL_RESULT = "No response available for this function call."


def _call_ids(content: types.Content | None) -> set[str | None]:
    if content is None:
        return set()
    return {p.function_call.id for p in content.parts or [] if p.function_call}


def _response_ids(content: types.Content | None) -> set[str | None]:
    if content is None:
        return set()
    return {p.function_response.id for p in content.parts or [] if p.function_response}


def _drop_orphaned_responses(contents: list[types.Content]) -> list[types.Content]:
    """Remove function_response parts whose call is not in the preceding content."""
    kept: list[types.Content] = []
    for index, content in enumerate(contents):
        responses = _response_ids(content)
        if not responses:
            kept.append(content)
            continue

        answerable = _call_ids(contents[index - 1]) if index > 0 else set()
        parts = [p for p in content.parts or [] if not p.function_response or p.function_response.id in answerable]
        if not parts:
            continue
        content.parts = parts
        kept.append(content)
    return kept


def _synthesize_missing_responses(contents: list[types.Content]) -> list[types.Content]:
    """Give every function_call a function_response in the immediately following content."""
    repaired: list[types.Content] = []
    for index, content in enumerate(contents):
        repaired.append(content)

        calls = [p.function_call for p in content.parts or [] if p.function_call]
        if not calls:
            continue

        following = contents[index + 1] if index + 1 < len(contents) else None
        answered = _response_ids(following)
        missing = [call for call in calls if call.id not in answered]
        if not missing:
            continue

        parts = [
            types.Part(
                function_response=types.FunctionResponse(
                    id=call.id,
                    name=call.name,
                    response={"result": MISSING_TOOL_RESULT},
                )
            )
            for call in missing
        ]

        # Join an existing response turn so the results stay in one message;
        # otherwise the results need a turn of their own, before whatever
        # currently follows.
        if following is not None and answered:
            following.parts = list(following.parts or []) + parts
        else:
            repaired.append(types.Content(role="user", parts=parts))
    return repaired


def repair_tool_call_pairing_callback(
    callback_context: CallbackContext,
    llm_request: LlmRequest,
) -> None:
    """Before-model callback that pairs every tool call with a tool result.

    Drops a result whose call is gone, then supplies a placeholder result for a
    call that has none, so the request satisfies the strict call/result pairing
    that Anthropic (and Bedrock) require.
    """
    if not llm_request.contents:
        return None
    contents = _drop_orphaned_responses(list(llm_request.contents))
    llm_request.contents = _synthesize_missing_responses(contents)
    return None
