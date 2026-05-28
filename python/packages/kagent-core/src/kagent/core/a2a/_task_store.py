import asyncio
import logging
from datetime import timezone

import httpx
from a2a.server.tasks import TaskStore
from a2a.types import ListTasksRequest, ListTasksResponse, Message, Task
from google.protobuf.json_format import MessageToDict, ParseDict
from typing_extensions import override

from kagent.core.a2a import read_metadata_value

logger = logging.getLogger(__name__)
DEFAULT_LIST_TASKS_PAGE_SIZE = 50


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
    async def save(self, task: Task, context=None) -> None:
        """Save a task to KAgent.

        Skips saving if the current event is a partial streaming chunk.
        The adk_partial flag is set on event.metadata by AgentExecutor and
        gets copied to task.metadata by TaskManager.

        Args:
            task: The task to save
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        Raises:
            httpx.HTTPStatusError: If the API request fails
        """
        # Clean any partial events from history before saving
        history = list(task.history or [])
        clean_history = self._clean_partial_events(history)
        if len(clean_history) != len(history):
            del task.history[:]
            task.history.extend(clean_history)

        response = await self.client.post(
            "/api/tasks",
            json=MessageToDict(task),
        )
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
        wrapped = response.json()
        data = wrapped.get("data") if isinstance(wrapped, dict) else None
        if not isinstance(data, dict):
            return None
        return ParseDict(data, Task())

    @override
    async def list(self, params: ListTasksRequest, context=None) -> ListTasksResponse:
        """List tasks for a context (session) from KAgent.

        The controller exposes task listing under the session-scoped endpoint,
        so ``params.context_id`` is required to fetch tasks.
        """
        page_size = params.page_size or DEFAULT_LIST_TASKS_PAGE_SIZE
        if not params.context_id:
            return ListTasksResponse(tasks=[], page_size=page_size, total_size=0)

        response = await self.client.get(f"/api/sessions/{params.context_id}/tasks")
        if response.status_code == 404:
            return ListTasksResponse(tasks=[], page_size=page_size, total_size=0)
        response.raise_for_status()

        wrapped = response.json()
        data = wrapped.get("data") if isinstance(wrapped, dict) else None
        if not isinstance(data, list):
            return ListTasksResponse(tasks=[], page_size=page_size, total_size=0)

        tasks: list[Task] = []
        for item in data:
            if not isinstance(item, dict):
                continue
            try:
                tasks.append(ParseDict(item, Task()))
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
