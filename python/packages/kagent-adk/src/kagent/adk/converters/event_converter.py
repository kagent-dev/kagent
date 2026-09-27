from __future__ import annotations

import logging
import uuid
from typing import Any, Dict, List, Optional

from a2a.server.events import Event as A2AEvent
from a2a.types import (
    Artifact,
    Message,
    Role,
    Task,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
)
from a2a.types import Part as A2APart
from google.adk.agents.invocation_context import InvocationContext
from google.adk.events.event import Event
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict
from kagent.core.a2a import (
    A2A_USAGE_METADATA_KEY,
    now_timestamp,
)

from .error_mappings import _get_error_message, _is_normal_completion
from .part_converter import (
    convert_genai_part_to_a2a_part,
)

# Constants

ARTIFACT_ID_SEPARATOR = "-"

# Logger
logger = logging.getLogger("kagent_adk." + __name__)


def serialize_metadata_value(value: Any) -> Any:
    """Safely serializes a metadata value for A2A message/event metadata.

    Pydantic values (anything with ``model_dump``) are returned as their
    JSON-compatible serialized ``dict`` so structured metadata such as
    ``usage_metadata`` stays machine-readable for consumers (for example the UI
    reads the token counts as object fields). Everything else is returned as its
    string representation.

    Args:
      value: The value to serialize.

    Returns:
      JSON-serializable representation of the value.
    """
    if hasattr(value, "DESCRIPTOR"):
        return MessageToDict(value)
    if hasattr(value, "model_dump"):
        try:
            return value.model_dump(mode="json", exclude_none=True, by_alias=True)
        except Exception as e:
            logger.warning("Failed to serialize metadata value: %s", e)
            return str(value)
    return str(value)


def _get_context_metadata(event: Event, invocation_context: InvocationContext) -> Dict[str, Any]:
    """Return the public event metadata understood outside the ADK adapter."""
    if not event:
        raise ValueError("Event cannot be None")
    if not invocation_context:
        raise ValueError("Invocation context cannot be None")

    if event.usage_metadata is None:
        return {}
    return {A2A_USAGE_METADATA_KEY: serialize_metadata_value(event.usage_metadata)}


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


def convert_event_to_a2a_message(
    event: Event,
    invocation_context: InvocationContext,
    role: Role = Role.ROLE_AGENT,
    task_id: Optional[str] = None,
    context_id: Optional[str] = None,
) -> Optional[Message]:
    """Converts an ADK event to an A2A message.

    Args:
      event: The ADK event to convert.
      invocation_context: The invocation context.
      role: The role attribute for the message (default: Role.agent).
      task_id: Optional task ID stamped onto the message so it carries the
        same identity as the enclosing task. A2A allows these to be omitted
        (the task is the canonical carrier), but stamping them lets consumers
        that flatten task.history into standalone messages key each message to
        its task without backfilling.
      context_id: Optional context ID stamped onto the message, as task_id.

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

        if a2a_parts:
            message_metadata = _get_context_metadata(event, invocation_context)
            return Message(
                message_id=str(uuid.uuid4()),
                role=role,
                parts=a2a_parts,
                metadata=message_metadata,
                task_id=task_id,
                context_id=context_id,
            )

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
    error_message = getattr(event, "error_message", None)

    event_metadata = _get_context_metadata(event, invocation_context)
    if event.error_code:
        if not error_message:
            error_message = _get_error_message(event.error_code)

    return TaskStatusUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        metadata=event_metadata,
        status=TaskStatus(
            state=TaskState.TASK_STATE_FAILED,
            message=Message(
                message_id=str(uuid.uuid4()),
                role=Role.ROLE_AGENT,
                parts=[A2APart(text=error_message)],
            ),
            timestamp=now_timestamp(),
        ),
    )


def _create_artifact_update_event(
    message: Message,
    invocation_context: InvocationContext,
    event: Event,
    task_id: Optional[str] = None,
    context_id: Optional[str] = None,
    agents_artifacts: Optional[Dict[str, str]] = None,
) -> Optional[TaskArtifactUpdateEvent]:
    """Creates a TaskArtifactUpdateEvent for task output.

    Args:
      message: The A2A message to include.
      invocation_context: The invocation context.
      event: The ADK event.
      task_id: Optional task ID to use for generated events.
      context_id: Optional Context ID to use for generated events.


    Returns:
      A TaskArtifactUpdateEvent containing the converted output parts.
    """
    metadata = _get_context_metadata(event, invocation_context)
    partial = bool(getattr(event, "partial", False))
    # Match Go adka2a.OutputArtifactPerEvent: reuse one artifact ID across
    # partial deltas, append while partial, then replace+close on the final
    # non-partial event (which may repeat the full text).
    artifact_id = str(uuid.uuid4())
    append = False
    if agents_artifacts is not None:
        agent_name = event.author or ""
        active_artifact_id = agents_artifacts.get(agent_name)
        if active_artifact_id:
            artifact_id = active_artifact_id
            append = partial
        if partial:
            agents_artifacts[agent_name] = artifact_id
        elif active_artifact_id:
            del agents_artifacts[agent_name]

    return TaskArtifactUpdateEvent(
        task_id=task_id,
        context_id=context_id,
        append=append,
        last_chunk=not partial,
        artifact=Artifact(
            artifact_id=artifact_id,
            parts=list(message.parts),
            metadata=metadata,
        ),
        metadata=metadata,
    )


def convert_event_to_a2a_events(
    event: Event,
    invocation_context: InvocationContext,
    task_id: Optional[str] = None,
    context_id: Optional[str] = None,
    agents_artifacts: Optional[Dict[str, str]] = None,
) -> List[A2AEvent]:
    """Converts a GenAI event to a list of A2A events.

    Args:
      event: The ADK event to convert.
      invocation_context: The invocation context.
      task_id: Optional task ID to use for generated events.
      context_id: Optional Context ID to use for generated events.
      agents_artifacts: Mutable mapping used to reuse artifact IDs across
        partial chunks from the same agent.

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
        if event.error_code and not _is_normal_completion(event.error_code):
            error_event = _create_error_status_event(event, invocation_context, task_id, context_id)
            a2a_events.append(error_event)
            return a2a_events

        # Handle regular message content
        message = convert_event_to_a2a_message(
            event,
            invocation_context,
            task_id=task_id,
            context_id=context_id,
        )
        if message:
            artifact_event = _create_artifact_update_event(
                message,
                invocation_context,
                event,
                task_id,
                context_id,
                agents_artifacts,
            )
            if artifact_event is not None:
                a2a_events.append(artifact_event)

    except Exception as e:
        logger.error("Failed to convert event to A2A events: %s", e)
        raise

    return a2a_events
