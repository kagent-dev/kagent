import asyncio

import httpx
from a2a.server.tasks import TaskStore
from a2a.types import Task
from pydantic import BaseModel
from typing_extensions import override


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

    @override
    async def save(self, task: Task, context=None) -> None:
        """Save a task to KAgent.

        Args:
            task: The task to save
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        response = await self.client.post("/api/tasks", json=task.model_dump(mode="json"))
        response.raise_for_status()

        # Signal that save completed (event-based sync)
        if task.id in self._save_events:
            self._save_events[task.id].set()

    @override
    async def get(self, task_id: str, context=None) -> Task | None:
        """Retrieve a task from KAgent.

        Args:
            task_id: The ID of the task to retrieve
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        Returns:
            The task if found, None otherwise

        Raises:
            httpx.HTTPStatusError: If the API request fails (except 404)
        """
        response = await self.client.get(f"/api/tasks/{task_id}")
        if response.status_code == 404:
            return None
        response.raise_for_status()

        # Unwrap the StandardResponse envelope from the Go controller
        wrapped = KAgentTaskResponse.model_validate(response.json())
        return wrapped.data

    @override
    async def delete(self, task_id: str, context=None) -> None:
        """Delete a task from KAgent.

        Args:
            task_id: The ID of the task to delete
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
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
