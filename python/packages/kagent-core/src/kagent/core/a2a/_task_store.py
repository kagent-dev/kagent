import asyncio
import logging

import httpx
from a2a.server.tasks import TaskStore
from a2a.types import Message, Task
from pydantic import BaseModel
from typing_extensions import override

from kagent.core.a2a import read_metadata_value

logger = logging.getLogger(__name__)


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

    Transient transport errors (idle keep-alive connections reset by a service
    mesh, controller pod restarts, etc.) are handled transparently: each HTTP
    operation is retried once after closing and re-opening the underlying
    connection.  Non-transport HTTP errors (4xx/5xx) are surfaced immediately
    without retrying so real failures are never swallowed.
    """

    # Maximum number of automatic retries for transient transport errors.
    _MAX_RETRIES = 1

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

    async def _request_with_retry(self, method: str, url: str, **kwargs) -> httpx.Response:
        """Execute an HTTP request, retrying once on transient transport errors.


        Args:
            method: HTTP method string ("GET", "POST", "DELETE", ...)
            url: Request URL (relative to the client's base_url)
            **kwargs: Extra keyword arguments forwarded to httpx.AsyncClient.request

        Returns:
            The successful httpx.Response.

        Raises:
            httpx.TransportError: If the transport error persists after all retries.
            httpx.HTTPStatusError: Propagated immediately without retrying.
        """
        last_exc: httpx.TransportError | None = None

        for attempt in range(self._MAX_RETRIES + 1):
            try:
                response = await self.client.request(method, url, **kwargs)
                return response
            except httpx.TransportError as exc:
                last_exc = exc
                logger.warning(
                    "TransportError on %s %s (attempt %d/%d): %s — will retry with a new connection",
                    method,
                    url,
                    attempt + 1,
                    self._MAX_RETRIES + 1,
                    exc,
                )

                # Don't close the shared AsyncClient here: it is reused across the process.
                # Just retry once; httpx will establish a new connection on the next request.

        # All retries exhausted — re-raise so the caller gets a clear error
        # instead of a silent drop.
        raise last_exc  # type: ignore[misc]

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
            httpx.HTTPStatusError: If the API request fails with a non-2xx status.
            httpx.TransportError: If a transport error persists after retries.
        """
        # Clean any partial events from history before saving
        history = task.history or []
        task.history = self._clean_partial_events(history)

        response = await self._request_with_retry(
            "POST",
            "/api/tasks",
            json=task.model_dump(mode="json"),
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
            httpx.HTTPStatusError: If the API request fails (except 404).
            httpx.TransportError: If a transport error persists after retries.
        """
        response = await self._request_with_retry("GET", f"/api/tasks/{task_id}")
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
            httpx.HTTPStatusError: If the API request fails.
            httpx.TransportError: If a transport error persists after retries.
        """
        response = await self._request_with_retry("DELETE", f"/api/tasks/{task_id}")
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
