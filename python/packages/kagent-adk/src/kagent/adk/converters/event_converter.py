from __future__ import annotations

import logging
import uuid
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional

from a2a.server.events import Event as A2AEvent
from a2a.types import DataPart, Message, Role, Task, TaskState, TaskStatus, TaskStatusUpdateEvent, TextPart
from a2a.types import Part as A2APart
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events.event import Event
from google.adk.flows.llm_flows.functions import REQUEST_EUC_FUNCTION_CALL_NAME
from google.genai import types as genai_types

from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)

from .part_converter import (
    convert_genai_part_to_a2a_part,
)

# Constants

ARTIFACT_ID_SEPARATOR = "-"
DEFAULT_ERROR_MESSAGE = "An error occurred during processing"

# Logger
logger = logging.getLogger("kagent_adk." + __name__)


def _serialize_metadata_value(value: Any) -> str:
    """Safely serializes metadata values to string format.

    Args:
      value: The value to serialize.

    Returns:
      String representation of the value.
    """
    if hasattr(value, "model_dump"):
        try:
            return value.model_dump(exclude_none=True, by_alias=True)
        except Exception as e:
            logger.warning("Failed to serialize metadata value: %s", e)
            return str(value)
    return str(value)


def _get_context_metadata(event: Event, invocation_context: InvocationContext) -> Dict[str, str]:
    """Gets the context metadata for the event.

    Args:
      event: The ADK event to extract metadata from.
      invocation_context: The invocation context containing session information.

    Returns:
      A dictionary containing the context metadata.

    Raises:
      ValueError: If required fields are missing from event or context.
    """
    if not event:
        raise ValueError("Event cannot be None")
    if not invocation_context:
        raise ValueError("Invocation context cannot be None")

    try:
        metadata = {
            get_kagent_metadata_key("app_name"): invocation_context.app_name,
            get_kagent_metadata_key("user_id"): invocation_context.user_id,
            get_kagent_metadata_key("session_id"): invocation_context.session.id,
            get_kagent_metadata_key("invocation_id"): event.invocation_id,
            get_kagent_metadata_key("author"): event.author,
        }

        # Add optional metadata fields if present
        optional_fields = [
            ("branch", event.branch),
            ("grounding_metadata", event.grounding_metadata),
            ("custom_metadata", event.custom_metadata),
            ("usage_metadata", event.usage_metadata),
            ("error_code", event.error_code),
        ]

        for field_name, field_value in optional_fields:
            if field_value is not None:
                metadata[get_kagent_metadata_key(field_name)] = _serialize_metadata_value(field_value)

        return metadata

    except Exception as e:
        logger.error("Failed to create context metadata: %s", e)
        raise


def _create_artifact_id(app_name: str, user_id: str, session_id: str, filename: str, version: int) -> str:
    """Creates a unique artifact ID.

    Args:
      app_name: The application name.
      user_id: The user ID.
      session_id: The session ID.
      filename: The artifact filename.
      version: The artifact version.

    Returns:
      A unique artifact ID string.
    """
    components = [app_name, user_id, session_id, filename, str(version)]
    return ARTIFACT_ID_SEPARATOR.join(components)


def _process_long_running_tool(a2a_part: A2APart, event: Event) -> None:
    """Processes long-running tool metadata for an A2A part.

    Args:
      a2a_part: The A2A part to potentially mark as long-running.
      event: The ADK event containing long-running tool information.
    """
    if (
        isinstance(a2a_part.root, DataPart)
        and event.long_running_tool_ids
        and a2a_part.root.metadata
        and a2a_part.root.metadata.get(get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY))
        == A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
        and a2a_part.root.data.get("id") in event.long_running_tool_ids
    ):
        a2a_part.root.metadata[get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY)] = True


def convert_event_to_a2a_message(
    event: Event, invocation_context: InvocationContext, role: Role = Role.agent
) -> Optional[Message]:
    """Converts an ADK event to an A2A message.

    Args:
      event: The ADK event to convert.
      invocation_context: The invocation context.

    Returns:
      An A2A Message if the event has content, None otherwise.

    Raises:
      ValueError: If required parameters are invalid.
    """
    if not event:
        raise ValueError("Event cannot be None")
    if not invocation_context:
        raise ValueError("Invocation context cannot be None")

    if not event.content or not event.content.parts:
        return None

    try:
        a2a_parts = []
        for part in event.content.parts:
            a2a_part = convert_genai_part_to_a2a_part(part)
            if a2a_part:
                a2a_parts.append(a2a_part)
                _process_long_running_tool(a2a_part, event)

        if a2a_parts:
            return Message(message_id=str(uuid.uuid4()), role=role, parts=a2a_parts)

    except Exception as e:
        logger.error("Failed to convert event to status message: %s", e)
        raise

    return None


def _create_error_status_event(
    event: Event,
    invocation_context: InvocationContext,
    task_id: Optional[str] = None,
    context_id: Optional[str] = None,
) -> TaskStatusUpdateEvent:
    """Creates a TaskStatusUpdateEvent for error scenarios.

    Args:
      event: The ADK event containing error information.
      invocation_context: The invocation context.
      task_id: Optional task ID to use for generated events.
      context_id: Optional Context ID to use for generated events.

    Returns:
      A TaskStatusUpdateEvent with FAILED state.
    """
    error_message = getattr(event, "error_message", None) or DEFAULT_ERROR_MESSAGE

    # Get context metadata and add error code
    event_metadata = _get_context_metadata(event, invocation_context)
    if event.error_code:
        event_metadata[get_kagent_metadata_key("error_code")] = str(event.error_code)

    return TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        metadata=event_metadata,
        status=TaskStatus(
            state=TaskState.failed,
            message=Message(
                message_id=str(uuid.uuid4()),
                role=Role.agent,
                parts=[A2APart(TextPart(text=error_message))],
                metadata={get_kagent_metadata_key("error_code"): str(event.error_code)} if event.error_code else {},
            ),
            timestamp=datetime.now(timezone.utc).isoformat(),
        ),
        final=False,
    )


def _create_status_update_event(
    message: Message,
    invocation_context: InvocationContext,
    event: Event,
    task_id: Optional[str] = None,
    context_id: Optional[str] = None,
) -> TaskStatusUpdateEvent:
    """Creates a TaskStatusUpdateEvent for running scenarios.

    Args:
      message: The A2A message to include.
      invocation_context: The invocation context.
      event: The ADK event.
      task_id: Optional task ID to use for generated events.
      context_id: Optional Context ID to use for generated events.


    Returns:
      A TaskStatusUpdateEvent with RUNNING state.
    """
    status = TaskStatus(
        state=TaskState.working,
        message=message,
        timestamp=datetime.now(timezone.utc).isoformat(),
    )

    if any(
        part.root.metadata.get(get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY))
        == A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
        and part.root.metadata.get(get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY)) is True
        and part.root.data.get("name") == REQUEST_EUC_FUNCTION_CALL_NAME
        for part in message.parts
        if part.root.metadata
    ):
        status.state = TaskState.auth_required
    elif any(
        part.root.metadata.get(get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY))
        == A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL
        and part.root.metadata.get(get_kagent_metadata_key(A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY)) is True
        for part in message.parts
        if part.root.metadata
    ):
        status.state = TaskState.input_required

    return TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        status=status,
        metadata=_get_context_metadata(event, invocation_context),
        final=False,
    )


def convert_event_to_a2a_events(
    event: Event,
    invocation_context: InvocationContext,
    task_id: Optional[str] = None,
    context_id: Optional[str] = None,
) -> List[A2AEvent]:
    """Converts a GenAI event to a list of A2A events.

    Args:
      event: The ADK event to convert.
      invocation_context: The invocation context.
      task_id: Optional task ID to use for generated events.
      context_id: Optional Context ID to use for generated events.

    Returns:
      A list of A2A events representing the converted ADK event.

    Raises:
      ValueError: If required parameters are invalid.
    """
    if not event:
        raise ValueError("Event cannot be None")
    if not invocation_context:
        raise ValueError("Invocation context cannot be None")

    a2a_events = []

    try:
        # Handle error scenarios
        if event.error_code:
            error_event = _create_error_status_event(event, invocation_context, task_id, context_id)
            a2a_events.append(error_event)

        # Handle regular message content
        message = convert_event_to_a2a_message(event, invocation_context)
        if message:
            running_event = _create_status_update_event(message, invocation_context, event, task_id, context_id)
            a2a_events.append(running_event)

    except Exception as e:
        logger.error("Failed to convert event to A2A events: %s", e)
        raise

    return a2a_events
