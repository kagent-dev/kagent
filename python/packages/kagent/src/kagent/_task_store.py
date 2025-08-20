from typing import override

import httpx
from a2a.server.tasks import TaskStore
from a2a.types import Task


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

    @override
    async def save(self, task: Task) -> None:
        """Save a task to KAgent.

        Args:
            task: The task to save

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        response = await self.client.post("/api/tasks", json=task.model_dump())
        response.raise_for_status()

    @override
    async def get(self, task_id: str) -> Task | None:
        """Retrieve a task from KAgent.

        Args:
            task_id: The ID of the task to retrieve

        Returns:
            The task if found, None otherwise

        Raises:
            httpx.HTTPStatusError: If the API request fails (except 404)
        """
        response = await self.client.get(f"/api/tasks/{task_id}")
        if response.status_code == 404:
            return None
        response.raise_for_status()
        return Task.model_validate(response.json())

    @override
    async def delete(self, task_id: str) -> None:
        """Delete a task from KAgent.

        Args:
            task_id: The ID of the task to delete

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        response = await self.client.delete(f"/api/tasks/{task_id}")
        response.raise_for_status()
