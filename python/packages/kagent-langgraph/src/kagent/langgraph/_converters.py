"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import json
import uuid
from datetime import UTC, datetime
from typing import Any

from a2a.types import (
    Message,
    Part,
    Role,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from langchain_core.messages import (
    AIMessage,
    HumanMessage,
    ToolMessage,
)


def _get_event_metadata(langgraph_event: dict[str, Any]) -> dict[str, Any]:
    """Get the metadata from a LangGraph event."""
    return {
        "app_name": langgraph_event.get("app_name", ""),
        "session_id": langgraph_event.get("session_id", ""),
    }


async def _convert_langgraph_event_to_a2a(
    langgraph_event: dict[str, Any], task_id: str, context_id: str, app_name: str
) -> list[TaskStatusUpdateEvent]:
    """Convert a LangGraph event to A2A events."""

    a2a_events: list[TaskStatusUpdateEvent] = []

    # LangGraph events have node names as keys, with 'messages' as values
    # Example: {'agent': {'messages': [AIMessage(...)]}}
    for node_name, node_data in langgraph_event.items():
        if not isinstance(node_data, dict) or "messages" not in node_data:
            continue
        messages = node_data["messages"]
        if not isinstance(messages, list):
            continue

        # Process each message in the event
        for message in messages:
            if isinstance(message, AIMessage):
                # Handle AI messages (assistant responses)
                a2a_message = Message(message_id=str(uuid.uuid4()), role=Role.agent, parts=[])
                if message.content and isinstance(message.content, str) and message.content.strip():
                    a2a_message.parts.append(Part(TextPart(text=message.content)))

                # Handle tool calls in AI messages
                if hasattr(message, "tool_calls") and message.tool_calls:
                    for tool_call in message.tool_calls:
                        a2a_message.parts.append(Part(TextPart(text=json.dumps(tool_call))))
                a2a_events.append(
                    TaskStatusUpdateEvent(
                        task_id=task_id,
                        status=TaskStatus(
                            state=TaskState.working,
                            timestamp=datetime.now(UTC).isoformat(),
                            message=a2a_message,
                        ),
                        context_id=context_id,
                        final=False,
                        metadata={
                            "app_name": app_name,
                            "session_id": context_id,
                        },
                    )
                )

            elif isinstance(message, ToolMessage):
                # Handle tool responses
                if message.content and isinstance(message.content, str):
                    tool_response_text = f"Tool '{message.name}' returned: {message.content}"
                    a2a_events.append(
                        TaskStatusUpdateEvent(
                            task_id=task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(UTC).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[Part(TextPart(text=tool_response_text))],
                                ),
                            ),
                            context_id=context_id,
                            final=False,
                            metadata={
                                "app_name": app_name,
                                "session_id": context_id,
                            },
                        )
                    )

            elif isinstance(message, HumanMessage):
                # Handle human messages (user input) - usually for context
                if message.content and isinstance(message.content, str) and message.content.strip():
                    a2a_events.append(
                        TaskStatusUpdateEvent(
                            task_id=task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(UTC).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[Part(TextPart(text=f"User: {message.content}"))],
                                ),
                            ),
                            context_id=context_id,
                            final=False,
                            metadata={
                                "app_name": app_name,
                                "session_id": context_id,
                            },
                        )
                    )
    return a2a_events
