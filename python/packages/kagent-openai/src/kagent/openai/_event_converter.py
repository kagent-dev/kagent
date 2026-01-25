"""Event converter for OpenAI Agents SDK to A2A protocol.

This module converts OpenAI Agents SDK streaming events to A2A protocol events.
"""

from __future__ import annotations

import json
import logging
import uuid
from datetime import UTC, datetime

from a2a.server.events import Event as A2AEvent
from a2a.types import (
    DataPart,
    Message,
    Role,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from a2a.types import Part as A2APart
from agents.items import MessageOutputItem, ToolCallItem, ToolCallOutputItem
from agents.stream_events import (
    AgentUpdatedStreamEvent,
    RawResponsesStreamEvent,
    RunItemStreamEvent,
    StreamEvent,
)

from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)

logger = logging.getLogger(__name__)


def convert_openai_event_to_a2a_events(
    event: StreamEvent,
    task_id: str,
    context_id: str,
    app_name: str,
) -> list[A2AEvent]:
    """Convert an OpenAI Agents SDK event to A2A events.

    Args:
        event: OpenAI SDK streaming event
        task_id: A2A task ID
        context_id: A2A context ID
        app_name: Application name for metadata

    Returns:
        List of A2A events (may be empty if event doesn't need conversion)
    """
    a2a_events: list[A2AEvent] = []

    try:
        # Handle RunItemStreamEvent (messages, tool calls, tool outputs)
        if isinstance(event, RunItemStreamEvent):
            a2a_events.extend(_convert_run_item_event(event, task_id, context_id, app_name))

        # Handle RawResponsesStreamEvent (raw LLM responses)
        elif isinstance(event, RawResponsesStreamEvent):
            # These are low-level events - can be logged but not converted
            logger.debug(f"Raw response event: {event.data}")

        # Handle AgentUpdatedStreamEvent (agent handoffs)
        elif isinstance(event, AgentUpdatedStreamEvent):
            a2a_events.extend(_convert_agent_updated_event(event, task_id, context_id, app_name))

        # Other event types
        else:
            logger.debug(f"Unhandled event type: {type(event).__name__}")

    except Exception as e:
        logger.error(f"Error converting OpenAI event to A2A: {e}", exc_info=True)
        # Don't raise - we want to continue processing other events

    return a2a_events


def _convert_run_item_event(
    event: RunItemStreamEvent,
    task_id: str,
    context_id: str,
    app_name: str,
) -> list[A2AEvent]:
    """Convert a RunItemStreamEvent to A2A events.

    Args:
        event: OpenAI run item stream event
        task_id: A2A task ID
        context_id: A2A context ID
        app_name: Application name

    Returns:
        List containing A2A events based on the item type
    """
    # Handle message output
    if isinstance(event.item, MessageOutputItem):
        return _convert_message_output(event.item, task_id, context_id, app_name)

    # Handle tool calls
    elif isinstance(event.item, ToolCallItem):
        return _convert_tool_call(event.item, task_id, context_id, app_name)

    # Handle tool outputs
    elif isinstance(event.item, ToolCallOutputItem):
        return _convert_tool_output(event.item, task_id, context_id, app_name)

    # Other item types
    else:
        logger.debug(f"Unhandled run item type: {type(event.item).__name__}")
        return []


def _convert_message_output(
    item: MessageOutputItem,
    task_id: str,
    context_id: str,
    app_name: str,
) -> list[A2AEvent]:
    """Convert a message output item to A2A event.

    MessageOutputItem.raw_item is a ResponseOutputMessage with content list.
    Each content item is either ResponseOutputText or ResponseOutputRefusal.
    """
    text_parts = []

    # Access the raw Pydantic model
    raw_message = item.raw_item

    # Iterate through content parts
    if hasattr(raw_message, "content") and raw_message.content:
        if isinstance(raw_message.content, str):
            text_parts.append(raw_message.content)
        else:
            for part in raw_message.content:
                # Check if this is a text part (ResponseOutputText has 'text' field)
                if hasattr(part, "text"):
                    text_parts.append(part.text)
                # Otherwise, it is ResponseOutputRefusal and the model will explain why
                elif hasattr(part, "refusal"):
                    text_parts.append(f"[Refusal] {part.refusal}")

    if not text_parts:
        return []

    text_content = "".join(text_parts)

    message = Message(
        message_id=str(uuid.uuid4()),
        role=Role.agent,
        parts=[A2APart(TextPart(text=text_content))],
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
            get_kagent_metadata_key("event_type"): "message_output",
        },
    )

    status_event = TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        status=TaskStatus(
            state=TaskState.working,
            message=message,
            timestamp=datetime.now(UTC).isoformat(),
        ),
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
        },
        final=False,
    )

    return [status_event]


def _convert_tool_call(
    item: ToolCallItem,
    task_id: str,
    context_id: str,
    app_name: str,
) -> list[A2AEvent]:
    """Convert a tool call item to A2A event.

    ToolCallItem.raw_item is typically ResponseFunctionToolCall with fields at top level:
    - name: str (tool name)
    - call_id: str (unique ID for this call)
    - arguments: str (JSON string)
    - id: Optional[str] (alternate ID field)
    """
    raw_call = item.raw_item

    # Extract tool call details from the raw item (fields are at top level)
    tool_name = raw_call.name if hasattr(raw_call, "name") else "unknown"
    call_id = (
        raw_call.call_id
        if hasattr(raw_call, "call_id")
        else (raw_call.id if hasattr(raw_call, "id") else str(uuid.uuid4()))
    )
    tool_arguments = {}

    # Arguments are a JSON string, need to parse them
    if hasattr(raw_call, "arguments"):
        try:
            tool_arguments = (
                json.loads(raw_call.arguments) if isinstance(raw_call.arguments, str) else raw_call.arguments
            )
        except (json.JSONDecodeError, TypeError):
            logger.warning(f"Failed to parse arguments: {raw_call.arguments}")
            tool_arguments = {"raw": str(raw_call.arguments)}

    # Create a DataPart for the function call
    # Note: Frontend expects 'args' not 'arguments', and 'id' for the call ID
    function_data = {
        "id": call_id,
        "name": tool_name,
        "args": tool_arguments,
    }

    data_part = DataPart(
        data=function_data,
        metadata={
            get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
        },
    )

    message = Message(
        message_id=str(uuid.uuid4()),
        role=Role.agent,
        parts=[A2APart(data_part)],
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
            get_kagent_metadata_key("event_type"): "tool_call",
        },
    )

    status_event = TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        status=TaskStatus(
            state=TaskState.working,
            message=message,
            timestamp=datetime.now(UTC).isoformat(),
        ),
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
        },
        final=False,
    )

    return [status_event]


def _convert_tool_output(
    item: ToolCallOutputItem,
    task_id: str,
    context_id: str,
    app_name: str,
) -> list[A2AEvent]:
    """Convert a tool output item to A2A event.

    ToolCallOutputItem contains:
    - raw_item: FunctionCallOutput | ComputerCallOutput | LocalShellCallOutput
    - output: The actual Python object returned by the tool
    """
    raw_output = item.raw_item

    # Extract tool output details from the raw item
    call_id = raw_output.call_id if hasattr(raw_output, "call_id") else str(uuid.uuid4())

    # item.output is the actual return value (Any)
    actual_output: str = item.output

    # Create a DataPart for the function response
    function_data = {
        "id": call_id,
        "name": call_id,  # Name is not returned by the tool
        "response": {"result": actual_output},
    }

    data_part = DataPart(
        data=function_data,
        metadata={
            get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
        },
    )

    message = Message(
        message_id=str(uuid.uuid4()),
        role=Role.agent,
        parts=[A2APart(data_part)],
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
            get_kagent_metadata_key("event_type"): "tool_output",
        },
    )

    status_event = TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        status=TaskStatus(
            state=TaskState.working,
            message=message,
            timestamp=datetime.now(UTC).isoformat(),
        ),
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
        },
        final=False,
    )

    return [status_event]


def _convert_agent_updated_event(
    event: AgentUpdatedStreamEvent,
    task_id: str,
    context_id: str,
    app_name: str,
) -> list[A2AEvent]:
    """Convert an agent updated event (handoff) to A2A event.

    This is converted to a function_call event so the frontend renders it
    using the AgentCallDisplay component. This is ideal if there are multiple handoffs.
    """
    agent_name = event.new_agent.name
    if "/" in agent_name:
        tool_name = agent_name.replace("/", "__NS__")
    else:
        tool_name = f"{agent_name}__NS__agent"

    function_data = {
        "id": str(uuid.uuid4()),
        "name": tool_name,
        "args": {"target_agent": agent_name},
    }

    data_part = DataPart(
        data=function_data,
        metadata={
            get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
        },
    )

    message = Message(
        message_id=str(uuid.uuid4()),
        role=Role.agent,
        parts=[A2APart(data_part)],
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
            get_kagent_metadata_key("event_type"): "agent_handoff",
            get_kagent_metadata_key("new_agent_name"): agent_name,
        },
    )

    status_event = TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        status=TaskStatus(
            state=TaskState.working,
            message=message,
            timestamp=datetime.now(UTC).isoformat(),
        ),
        metadata={
            get_kagent_metadata_key("app_name"): app_name,
        },
        final=False,
    )

    return [status_event]
