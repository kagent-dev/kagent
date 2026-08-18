import asyncio

import httpx
from a2a.server.context import ServerCallContext
from a2a.server.tasks import TaskStore
from a2a.types import Message, Task
from pydantic import BaseModel
from typing_extensions import override

from kagent.core.a2a import read_metadata_value

from ._context import get_call_context_user_id, scoped_request_user_id


class KAgentTaskResponse(BaseModel):
    """Wrapper for KAgent controller API responses.

    The KAgent Go controller wraps all task responses in a StandardResponse envelope
    with the format: {"error": bool, "data": T, "message": str}.
    This model unwraps that envelope to extract the actual Task object.
    """

    error: bool
    data: Task | None = None
    message: str | None = None


class KAgentTaskStore(TaskStore):
    """
    A task store that persists A2A tasks to KAgent via REST API.
    """

    def __init__(self, client: httpx.AsyncClient):
        """Initialize the task store.

        Args:
            client: HTTP client configured with KAgent base URL
        """
        self.client = client
        # Event-based sync: track pending save operations
        self._save_events: dict[str, asyncio.Event] = {}

    def _is_partial_event(self, item: Message) -> bool:
        """Check if a history item is a partial ADK streaming event."""
        metadata = item.metadata or {}
        return read_metadata_value(metadata, "adk_partial") is True

    def _clean_partial_events(self, history: list[Message]) -> list[Message]:
        """Remove partial streaming events from history."""
        return [item for item in history if not self._is_partial_event(item)]

    @override
    async def save(self, task: Task, context: ServerCallContext | None = None) -> None:
        """Save a task to KAgent.

        Skips saving if the current event is a partial streaming chunk.
        The adk_partial flag is set on event.metadata by AgentExecutor and
        gets copied to task.metadata by TaskManager.

        Args:
            task: The task to save
            context: Server call context supplying the effective user, when available

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        # Clean any partial events from history before saving
        history = task.history or []
        task.history = self._clean_partial_events(history)

        with scoped_request_user_id(get_call_context_user_id(context)):
            response = await self.client.post("/api/tasks", json=task.model_dump(mode="json"))
        response.raise_for_status()

        # Signal that save completed (event-based sync)
        if task.id in self._save_events:
            self._save_events[task.id].set()

    @override
    async def get(self, task_id: str, context: ServerCallContext | None = None) -> Task | None:
        """Retrieve a task from KAgent.

        Args:
            task_id: The ID of the task to retrieve
            context: Server call context supplying the effective user, when available

        Returns:
            The task if found, None otherwise

        Raises:
            httpx.HTTPStatusError: If the API request fails (except 404)
        """
        with scoped_request_user_id(get_call_context_user_id(context)):
            response = await self.client.get(f"/api/tasks/{task_id}")
        if response.status_code == 404:
            return None
        response.raise_for_status()

        # Unwrap the StandardResponse envelope from the Go controller
        wrapped = KAgentTaskResponse.model_validate(response.json())
        return wrapped.data

    @override
    async def delete(self, task_id: str, context: ServerCallContext | None = None) -> None:
        """Delete a task from KAgent.

        Args:
            task_id: The ID of the task to delete
            context: Server call context supplying the effective user, when available

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        with scoped_request_user_id(get_call_context_user_id(context)):
            response = await self.client.delete(f"/api/tasks/{task_id}")
        response.raise_for_status()

    async def wait_for_save(self, task_id: str, timeout: float = 5.0) -> None:
        """Wait for a task to be saved (event-based sync).

        This method is used to synchronize with the save operation instead of
        using arbitrary sleep delays. It's particularly useful after interrupts
        to ensure the task state is persisted before resuming.

        Args:
            task_id: The ID of the task to wait for
            timeout: Maximum time to wait in seconds (default: 5.0)

        Raises:
            asyncio.TimeoutError: If the save doesn't complete within timeout
        """
        event = asyncio.Event()
        self._save_events[task_id] = event
        try:
            await asyncio.wait_for(event.wait(), timeout=timeout)
        finally:
            # Clean up the event
            self._save_events.pop(task_id, None)
