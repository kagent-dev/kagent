"""Kagent A2A protocol client"""

import httpx
import uuid
import json
from typing import Any, AsyncIterator
from structlog import get_logger

logger = get_logger(__name__)


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
    ) -> dict[str, Any]:
        """
        Invoke agent via A2A protocol (synchronous)

        Args:
            namespace: Agent namespace (e.g., "kagent")
            agent_name: Agent name (e.g., "k8s-agent")
            message: User message text
            session_id: Session ID for context
            user_id: User ID for authentication

        Returns:
            A2A protocol Task response
        """
        url = f"{self.base_url}/api/a2a/{namespace}/{agent_name}/"

        # JSON-RPC 2.0 request
        request = {
            "jsonrpc": "2.0",
            "method": "message/send",
            "params": {
                "message": {
                    "kind": "message",
                    "role": "user",
                    "parts": [{"kind": "text", "text": message}],
                    "context_id": session_id,
                }
            },
            "id": str(uuid.uuid4()),
        }

        headers = {
            "Content-Type": "application/json",
            "X-User-Id": user_id,
        }

        logger.info(
            "Invoking agent",
            namespace=namespace,
            agent=agent_name,
            user_id=user_id,
            session_id=session_id,
        )

        try:
            response = await self.client.post(url, json=request, headers=headers)
            response.raise_for_status()

            result: dict[str, Any] = response.json()

            logger.info(
                "Agent invocation successful",
                namespace=namespace,
                agent=agent_name,
                status_code=response.status_code,
            )

            return result

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
                "Agent invocation error",
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
    ) -> AsyncIterator[dict[str, Any]]:
        """
        Invoke agent via A2A protocol (streaming)

        Args:
            namespace: Agent namespace
            agent_name: Agent name
            message: User message text
            session_id: Session ID for context
            user_id: User ID for authentication

        Yields:
            SSE events from agent
        """
        url = f"{self.base_url}/api/a2a/{namespace}/{agent_name}/"

        request = {
            "jsonrpc": "2.0",
            "method": "message/stream",
            "params": {
                "message": {
                    "kind": "message",
                    "role": "user",
                    "parts": [{"kind": "text", "text": message}],
                    "context_id": session_id,
                }
            },
            "id": str(uuid.uuid4()),
        }

        headers = {
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
            "X-User-Id": user_id,
        }

        logger.info(
            "Streaming from agent",
            namespace=namespace,
            agent=agent_name,
            user_id=user_id,
            session_id=session_id,
        )

        async with self.streaming_client.stream("POST", url, json=request, headers=headers) as response:
            response.raise_for_status()

            async for line in response.aiter_lines():
                if line.startswith("data: "):
                    data = line[6:]
                    if data.strip() and data.strip() != "[DONE]":
                        try:
                            yield json.loads(data)
                        except json.JSONDecodeError as e:
                            logger.warning("Failed to parse SSE data", error=str(e), data=data)

    async def close(self) -> None:
        """Close HTTP clients"""
        await self.client.aclose()
        await self.streaming_client.aclose()
