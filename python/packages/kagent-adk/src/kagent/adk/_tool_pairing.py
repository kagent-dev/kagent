"""Tool call/response pairing repair for the model request.

A tool call and its result are persisted as two separate session events. If a
turn ends between them (process restart, OOM, client disconnect, cancellation),
the session keeps a ``function_call`` with no matching ``function_response``.

History is replayed on every later turn, and providers that require strict
pairing reject the whole conversation. Anthropic returns ``tool_use ids were
found without tool_result blocks immediately after`` and the session stays
unusable until it is deleted.

The repair runs against the model request rather than the store, so recorded
history stays intact and sessions already broken in the field heal on their next
turn without a migration. It also sits above the session service, so it applies
whichever store backs the session.

ADK hands each request session-isolated copies of the content
(``_copy_content_for_request`` in ``flows/llm_flows/contents.py``). Those copies
are **shallow**: ``Content`` and every ``Part`` are copied, but nested payloads
(``function_call.args``, ``function_response.response``, ``inline_data.data``)
are shared with the session events. This module may therefore only assign
``Content.parts`` or append new ``Part`` objects. Mutating a nested field in
place would corrupt persisted history.

Pairing is checked positionally, against the immediately following content,
because that is the invariant the provider enforces.

On a conversation whose calls are all answered this is a no-op, and any
conversation it does change is one the provider would have rejected outright.
"""

from __future__ import annotations

import logging

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.genai import types

logger = logging.getLogger(__name__)

# Stands in for a result that was never recorded. States no cause, because the
# call may have been interrupted or the process may simply have died between the
# two events. Matches the placeholder the Anthropic and OpenAI converters
# already use, so providers that repair on their own keep the same behaviour.
MISSING_TOOL_RESULT = "No response available for this function call."

# Stands in for a call that ADK is deliberately holding open: a long-running
# tool, a human approval, or ask_user. ADK creates no function_response for
# these until the answer arrives, so the call is pending rather than lost. The
# distinction matters: told a tool "returned nothing", a model reissues it or
# proceeds; told the call is still awaiting a response, it can wait.
PENDING_TOOL_RESULT = "This call is awaiting a response and has not completed yet."


def _pending_call_ids(callback_context: CallbackContext | None) -> set[str]:
    """Ids of calls ADK is holding open for a long-running tool or an approval.

    Read from the session events rather than the request, because
    ``long_running_tool_ids`` lives on the event and does not survive the
    conversion to contents.
    """
    session = getattr(callback_context, "session", None)
    if session is None:
        return set()
    pending: set[str] = set()
    for event in session.events or []:
        if event.long_running_tool_ids:
            pending.update(event.long_running_tool_ids)
    return pending


def _response_ids(content: types.Content | None) -> list[str | None]:
    if content is None:
        return []
    return [p.function_response.id for p in content.parts or [] if p.function_response]


def _placeholder_for(call: types.FunctionCall, pending_ids: set[str]) -> str:
    if call.id and call.id in pending_ids:
        return PENDING_TOOL_RESULT
    return MISSING_TOOL_RESULT


def _unanswered(calls: list[types.FunctionCall], answered: list[str | None]) -> list[types.FunctionCall]:
    """Calls with no response in ``answered``.

    Ids are matched by consuming them one at a time rather than through a set,
    so a turn carrying several calls with no id (Gemini omits them, and ADK
    strips its own ``adk-`` ids before the request is built) does not have one
    response silently answer all of them.
    """
    remaining = list(answered)
    missing = []
    for call in calls:
        if call.id in remaining:
            remaining.remove(call.id)
            continue
        missing.append(call)
    return missing


def repair_tool_call_pairing_callback(
    callback_context: CallbackContext,
    llm_request: LlmRequest,
) -> None:
    """Before-model callback that pairs every tool call with a tool result.

    Supplies a placeholder result for any call the immediately following content
    does not answer, so the request satisfies the strict call/result pairing that
    Anthropic and Bedrock require.
    """
    contents = llm_request.contents
    if not contents:
        return None

    pending_ids = _pending_call_ids(callback_context)
    synthesized = 0
    repaired: list[types.Content] = []
    for index, content in enumerate(contents):
        repaired.append(content)

        calls = [p.function_call for p in content.parts or [] if p.function_call]
        if not calls:
            continue

        following = contents[index + 1] if index + 1 < len(contents) else None
        answered = _response_ids(following)
        missing = _unanswered(calls, answered)
        if not missing:
            continue
        synthesized += len(missing)

        parts = [
            types.Part(
                function_response=types.FunctionResponse(
                    id=call.id,
                    name=call.name,
                    response={"result": _placeholder_for(call, pending_ids)},
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

    llm_request.contents = repaired
    if synthesized:
        # Logged because a repaired request means a turn ended between a tool
        # call and its result somewhere upstream.
        logger.info("Paired %d unanswered tool call(s) before the model request", synthesized)
    return None
