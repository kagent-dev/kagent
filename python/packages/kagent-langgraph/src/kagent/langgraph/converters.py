"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""

import asyncio
import logging
import uuid
from datetime import UTC, datetime
from typing import Any, override

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    Message,
    Part,
    Role,
    TaskArtifactUpdateEvent,
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


async def _convert_langgraph_event_to_a2a(
    langgraph_event: dict[str, Any], task_id: str, context_id: str, app_name: str
) -> TaskStatusUpdateEvent | None:
    """Convert a LangGraph event to A2A events."""

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
                        tool_call_text = f"Calling tool: {tool_call['name']} with args: {tool_call['args']}"
                        a2a_message.parts.append(Part(TextPart(text=tool_call_text)))
                return TaskStatusUpdateEvent(
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

            elif isinstance(message, ToolMessage):
                # Handle tool responses
                if message.content and isinstance(message.content, str):
                    tool_response_text = f"Tool '{message.name}' returned: {message.content}"
                    return TaskStatusUpdateEvent(
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

            elif isinstance(message, HumanMessage):
                # Handle human messages (user input) - usually for context
                if message.content and isinstance(message.content, str) and message.content.strip():
                    return TaskStatusUpdateEvent(
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
    return None
