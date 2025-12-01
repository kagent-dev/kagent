"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import hashlib
import uuid
from datetime import UTC, datetime
from typing import Any

from a2a.types import (
    DataPart,
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

from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)

from ._metadata_utils import get_rich_event_metadata


async def _convert_langgraph_event_to_a2a(
    langgraph_event: dict[str, Any],
    task_id: str,
    context_id: str,
    app_name: str,
    sent_message_ids: set[str],
) -> list[TaskStatusUpdateEvent]:
    """Convert a LangGraph event to A2A events.

    Deduplicates messages using sent_message_ids to avoid replaying history.
    """
    a2a_events: list[TaskStatusUpdateEvent] = []

    # LangGraph events have node names as keys, with 'messages' as values
    # Example: {'agent': {'messages': [AIMessage(...)]}}
    for node_name, node_data in langgraph_event.items():
        if not isinstance(node_data, dict) or "messages" not in node_data:
            continue
        messages = node_data["messages"]
        if not isinstance(messages, list):
            continue

        for message in messages:
            # Deduplicate using content hash (message.id is often None)
            msg_content = f"{type(message).__name__}:{message.content}"
            if hasattr(message, "tool_calls") and message.tool_calls:
                msg_content += f":tools:{len(message.tool_calls)}"
            msg_id = hashlib.md5(msg_content.encode()).hexdigest()

            if msg_id in sent_message_ids:
                continue
            sent_message_ids.add(msg_id)

            if isinstance(message, AIMessage):
                # Handle AI messages (assistant responses)
                a2a_message = Message(message_id=str(uuid.uuid4()), role=Role.agent, parts=[])
                if message.content and isinstance(message.content, str) and message.content.strip():
                    a2a_message.parts.append(Part(TextPart(text=message.content)))

                # Handle tool calls in AI messages
                if hasattr(message, "tool_calls") and message.tool_calls:
                    for tool_call in message.tool_calls:
                        a2a_message.parts.append(
                            Part(
                                DataPart(
                                    data={
                                        "id": tool_call["id"],
                                        "name": tool_call["name"],
                                        "args": tool_call["args"],
                                    },
                                    metadata={
                                        get_kagent_metadata_key(
                                            A2A_DATA_PART_METADATA_TYPE_KEY
                                        ): A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
                                    },
                                )
                            )
                        )

                # Only send message if it has parts (content or tool calls)
                if not a2a_message.parts:
                    continue

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
                        metadata=get_rich_event_metadata(
                            app_name=app_name,
                            session_id=context_id,
                        ),
                    )
                )

            elif isinstance(message, ToolMessage):
                # Handle tool responses
                if message.content:
                    a2a_events.append(
                        TaskStatusUpdateEvent(
                            task_id=task_id,
                            status=TaskStatus(
                                state=TaskState.working,
                                timestamp=datetime.now(UTC).isoformat(),
                                message=Message(
                                    message_id=str(uuid.uuid4()),
                                    role=Role.agent,
                                    parts=[
                                        Part(
                                            DataPart(
                                                data={
                                                    "id": message.tool_call_id,
                                                    "name": message.name,
                                                    "response": message.content,
                                                },
                                                metadata={
                                                    get_kagent_metadata_key(
                                                        A2A_DATA_PART_METADATA_TYPE_KEY
                                                    ): A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
                                                },
                                            )
                                        )
                                    ],
                                ),
                            ),
                            context_id=context_id,
                            final=False,
                            metadata=get_rich_event_metadata(
                                app_name=app_name,
                                session_id=context_id,
                            ),
                        )
                    )

            elif isinstance(message, HumanMessage):
                # Skip - user input is already known by caller
                pass
    return a2a_events
