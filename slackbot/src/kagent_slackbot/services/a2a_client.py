"""Kagent A2A protocol client"""

import json
import uuid
from typing import Any, AsyncIterator

import httpx

# Import A2A SDK types
from a2a.types import (
    Message,
    MessageSendParams,
    Part,
    SendMessageRequest,
    SendStreamingMessageRequest,
    Task,
    TaskArtifactUpdateEvent,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from structlog import get_logger

logger = get_logger(__name__)


class A2AResponse:
    """Wrapper for JSON-RPC response from A2A API.

    The API returns: {"jsonrpc": "2.0", "id": "...", "result": {...}}
    This class extracts the result field which contains the actual Task or event data.
    """

    def __init__(self, response_dict: dict[str, Any]):
        self.jsonrpc = response_dict.get("jsonrpc", "2.0")
        self.id = response_dict.get("id")
        self.result = response_dict.get("result", {})

        # Check for JSON-RPC errors
        if "error" in response_dict:
            error = response_dict["error"]
            raise ValueError(f"A2A JSON-RPC error: {error}")


class A2AClient:
    """Client for Kagent A2A protocol (JSON-RPC 2.0)"""

    def __init__(self, base_url: str, timeout: int = 30):
        self.base_url = base_url.rstrip("/")
        self.client = httpx.AsyncClient(timeout=timeout)
        # Longer timeout for streaming (agents can take time to respond)
        self.streaming_client = httpx.AsyncClient(timeout=120)

    async def invoke_agent(
        self,
        namespace: str,
        agent_name: str,
        message: str,
        session_id: str,
        user_id: str,
        task_id: str | None = None,
    ) -> Task:
        """
        Invoke an agent synchronously and return the Task.

        Args:
            namespace: Agent namespace (e.g., "kagent")
            agent_name: Agent name (e.g., "k8s-agent")
            message: User message text
            session_id: Session ID for context
            user_id: User ID for authentication
            task_id: Optional task ID to resume existing task

        Returns:
            Task: Typed Task object with history and status
        """
        url = f"{self.base_url}/api/a2a/{namespace}/{agent_name}/"

        # Build message using SDK types
        parts: list[Part] = [TextPart(text=message)]

        msg = Message(
            message_id=str(uuid.uuid4()),
            role="user",
            parts=parts,
            context_id=session_id,
            task_id=task_id,
        )

        # Build request using SDK types
        params = MessageSendParams(message=msg)
        request = SendMessageRequest(
            id=str(uuid.uuid4()),
            params=params,
        )

        # Serialize with camelCase aliases
        request_dict = request.model_dump(by_alias=True)

        headers = {
            "Content-Type": "application/json",
            "X-User-Id": user_id,
        }

        logger.info(
            "Invoking agent",
            namespace=namespace,
            agent=agent_name,
            session=session_id,
            task_id=task_id,
        )

        try:
            response = await self.client.post(url, json=request_dict, headers=headers)
            response.raise_for_status()

            # Parse JSON-RPC response
            response_data = response.json()
            a2a_response = A2AResponse(response_data)

            # Validate and return typed Task
            task = Task.model_validate(a2a_response.result)

            logger.info(
                "Agent invocation complete",
                namespace=namespace,
                agent=agent_name,
                task_id=task.id,
                state=task.status.state,
            )

            return task

        except httpx.HTTPStatusError as e:
            logger.error(
                "Agent invocation failed",
                namespace=namespace,
                agent=agent_name,
                status_code=e.response.status_code,
                error=str(e),
            )
            raise
        except Exception as e:
            logger.error(
                "Agent invocation failed",
                namespace=namespace,
                agent=agent_name,
                error=str(e),
            )
            raise

    async def stream_agent(
        self,
        namespace: str,
        agent_name: str,
        message: str,
        session_id: str,
        user_id: str,
        task_id: str | None = None,
    ) -> AsyncIterator[TaskStatusUpdateEvent | TaskArtifactUpdateEvent]:
        """
        Stream agent responses as typed events.

        Args:
            namespace: Agent namespace
            agent_name: Agent name
            message: User message text
            session_id: Session ID for context
            user_id: User ID for authentication
            task_id: Optional task ID to resume existing task

        Yields:
            TaskStatusUpdateEvent | TaskArtifactUpdateEvent: Typed events from agent
        """
        url = f"{self.base_url}/api/a2a/{namespace}/{agent_name}/"

        # Build message using SDK types
        parts: list[Part] = [TextPart(text=message)]

        msg = Message(
            message_id=str(uuid.uuid4()),
            role="user",
            parts=parts,
            context_id=session_id,
            task_id=task_id,
        )

        # Build streaming request
        params = MessageSendParams(message=msg)
        request = SendStreamingMessageRequest(
            id=str(uuid.uuid4()),
            params=params,
        )

        request_dict = request.model_dump(by_alias=True)

        headers = {
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
            "X-User-Id": user_id,
        }

        logger.info(
            "Starting agent stream",
            namespace=namespace,
            agent=agent_name,
            session=session_id,
            task_id=task_id,
        )

        async with self.streaming_client.stream("POST", url, json=request_dict, headers=headers) as response:
            response.raise_for_status()

            async for line in response.aiter_lines():
                if line.startswith("data: "):
                    data = line[6:]
                    if data.strip() and data.strip() != "[DONE]":
                        try:
                            # Parse SSE event
                            event_dict = json.loads(data)

                            # Extract result from JSON-RPC wrapper
                            a2a_response = A2AResponse(event_dict)

                            # Try to validate as status-update or artifact-update
                            kind = a2a_response.result.get("kind")
                            if kind == "status-update":
                                event = TaskStatusUpdateEvent.model_validate(a2a_response.result)
                                yield event
                            elif kind == "artifact-update":
                                event = TaskArtifactUpdateEvent.model_validate(a2a_response.result)
                                yield event
                            else:
                                logger.warning("Unknown event kind", kind=kind)

                        except json.JSONDecodeError as e:
                            logger.warning("Failed to parse SSE data", error=str(e), data=data)
                        except Exception as e:
                            logger.warning("Failed to validate event", error=str(e), data=data)

    async def stream_agent_with_parts(
        self,
        namespace: str,
        agent_name: str,
        parts: list[Part],  # Now typed!
        session_id: str,
        user_id: str,
        task_id: str | None = None,
    ) -> AsyncIterator[TaskStatusUpdateEvent | TaskArtifactUpdateEvent]:
        """
        Stream agent responses with structured message parts.

        Args:
            namespace: Agent namespace
            agent_name: Agent name
            parts: List of typed Part objects (TextPart, DataPart, etc.)
            session_id: Session ID for context
            user_id: User ID for authentication
            task_id: Optional task ID to resume existing task

        Yields:
            TaskStatusUpdateEvent | TaskArtifactUpdateEvent: Typed events from agent
        """
        url = f"{self.base_url}/api/a2a/{namespace}/{agent_name}/"

        # Build message with provided parts
        msg = Message(
            message_id=str(uuid.uuid4()),
            role="user",
            parts=parts,
            context_id=session_id,
            task_id=task_id,
        )

        # Build streaming request
        params = MessageSendParams(message=msg)
        request = SendStreamingMessageRequest(
            id=str(uuid.uuid4()),
            params=params,
        )

        request_dict = request.model_dump(by_alias=True)

        headers = {
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
            "X-User-Id": user_id,
        }

        logger.info(
            "Starting agent stream with parts",
            namespace=namespace,
            agent=agent_name,
            session=session_id,
            task_id=task_id,
            num_parts=len(parts),
        )

        async with self.streaming_client.stream("POST", url, json=request_dict, headers=headers) as response:
            response.raise_for_status()

            async for line in response.aiter_lines():
                if line.startswith("data: "):
                    data = line[6:]
                    if data.strip() and data.strip() != "[DONE]":
                        try:
                            event_dict = json.loads(data)
                            a2a_response = A2AResponse(event_dict)

                            # Try to validate as status-update or artifact-update
                            kind = a2a_response.result.get("kind")
                            if kind == "status-update":
                                event = TaskStatusUpdateEvent.model_validate(a2a_response.result)
                                yield event
                            elif kind == "artifact-update":
                                event = TaskArtifactUpdateEvent.model_validate(a2a_response.result)
                                yield event
                            else:
                                logger.warning("Unknown event kind", kind=kind)

                        except json.JSONDecodeError as e:
                            logger.warning("Failed to parse SSE data", error=str(e), data=data)
                        except Exception as e:
                            logger.warning("Failed to validate event", error=str(e), data=data)

    async def close(self) -> None:
        """Close HTTP clients"""
        await self.client.aclose()
        await self.streaming_client.aclose()
