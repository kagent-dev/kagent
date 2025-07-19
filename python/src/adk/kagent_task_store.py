import httpx
from a2a.server.tasks import TaskStore
from a2a.types import Task


class KAgentTaskStore(TaskStore):
    client: httpx.AsyncClient

    def __init__(self, base_url: str):
        self.client = httpx.AsyncClient(base_url=base_url)

    async def save(self, task: Task) -> None:
        await self.client.post("/tasks", json=task.model_dump())

    async def get(self, task_id: str) -> Task | None:
        response = await self.client.get(f"/tasks/{task_id}")
        return Task.model_validate(response.json())

    async def delete(self, task_id: str) -> None:
        await self.client.delete(f"/tasks/{task_id}")
