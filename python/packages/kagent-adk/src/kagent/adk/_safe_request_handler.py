import asyncio
import logging

from a2a.server.request_handlers import DefaultRequestHandler

logger = logging.getLogger(__name__)


class SafeRequestHandler(DefaultRequestHandler):
    """Request handler that avoids turning cleanup cancellation into 500s."""

    async def _cleanup_producer(self, producer_task, task_id):
        try:
            await super()._cleanup_producer(producer_task, task_id)
        except asyncio.CancelledError:
            logger.debug(
                "A2A cleanup cancelled",
                extra={"task_id": task_id},
                exc_info=True,
            )
            # Make a best-effort attempt to clean up resources.
            await self._queue_manager.close(task_id)
            async with self._running_agents_lock:
                self._running_agents.pop(task_id, None)
