import asyncio
import logging
from datetime import timezone

import grpc
from a2a.server.tasks import TaskStore
from a2a.types import ListTasksRequest, ListTasksResponse, Task
from a2a.types import a2a_pb2
from a2a.utils.constants import DEFAULT_LIST_TASKS_PAGE_SIZE
from kagent.api.v1alpha1 import sessions_pb2
from typing_extensions import override

from .._grpc import AsyncControllerClient

logger = logging.getLogger(__name__)


class KAgentTaskStore(TaskStore):
    """
    A task store that persists A2A tasks to KAgent via gRPC.
    """

    def __init__(self, client: AsyncControllerClient):
        """Initialize the task store.

        Args:
            client: gRPC client configured with the KAgent controller address
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
        encoded = a2a_pb2.Task.FromString(task.SerializeToString())
        await self.client.task_service.UpsertTask(
            sessions_pb2.UpsertTaskRequest(task=encoded),
            **await self.client.call_options(),
        )

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
        try:
            response = await self.client.task_service.GetTask(
                sessions_pb2.GetTaskRequest(task_id=task_id),
                **await self.client.call_options(),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                return None
            raise
        return Task.FromString(response.task.SerializeToString())

    @override
    async def list(self, params: ListTasksRequest, context=None) -> ListTasksResponse:
        """List tasks for a context (session) from KAgent."""
        page_size = params.page_size or DEFAULT_LIST_TASKS_PAGE_SIZE
        if not params.context_id:
            return ListTasksResponse(tasks=[], page_size=page_size, total_size=0)

        tasks: list[Task] = []
        response = await self.client.task_service.ListTasks(
            sessions_pb2.ListTasksRequest(session_id=params.context_id),
            **await self.client.call_options(),
        )
        for item in response.tasks:
            try:
                tasks.append(Task.FromString(item.SerializeToString()))
            except Exception as err:
                logger.warning("Failed to parse task from list response: %s", err)

        if params.status:
            tasks = [task for task in tasks if task.status and task.status.state == params.status]

        if params.HasField("status_timestamp_after"):
            after = params.status_timestamp_after.ToDatetime().astimezone(timezone.utc)
            filtered: list[Task] = []
            for task in tasks:
                if not task.status or not task.status.HasField("timestamp"):
                    continue
                task_ts = task.status.timestamp.ToDatetime().astimezone(timezone.utc)
                if task_ts >= after:
                    filtered.append(task)
            tasks = filtered

        start = 0
        if params.page_token:
            try:
                start = max(0, int(params.page_token))
            except ValueError:
                start = 0
        if start >= len(tasks):
            return ListTasksResponse(tasks=[], page_size=page_size, total_size=len(tasks))

        end = min(start + page_size, len(tasks))
        next_page_token = str(end) if end < len(tasks) else ""
        return ListTasksResponse(
            tasks=tasks[start:end],
            page_size=page_size,
            total_size=len(tasks),
            next_page_token=next_page_token,
        )

    @override
    async def delete(self, task_id: str, context=None) -> None:
        """Delete a task from KAgent.

        Args:
            task_id: The ID of the task to delete
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        await self.client.task_service.DeleteTask(
            sessions_pb2.DeleteTaskRequest(task_id=task_id),
            **await self.client.call_options(),
        )

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
