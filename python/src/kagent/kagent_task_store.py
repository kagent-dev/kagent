from typing import override

import httpx
from a2a.server.tasks import TaskStore
from a2a.types import Task


class KAgentTaskStore(TaskStore):
    client: httpx.AsyncClient

    def __init__(self, client: httpx.AsyncClient):
        self.client = client

    @override
    async def save(self, task: Task) -> None:
        response = await self.client.post("/api/tasks", json=task.model_dump())
        response.raise_for_status()

    @override
    async def get(self, task_id: str) -> Task | None:
        response = await self.client.get(f"/api/tasks/{task_id}")
        if response.status_code == 404:
            return None
        response.raise_for_status()
        return Task.model_validate(response.json())

    @override
    async def delete(self, task_id: str) -> None:
        response = await self.client.delete(f"/api/tasks/{task_id}")
        response.raise_for_status()
