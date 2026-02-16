import asyncio
import logging
from typing import Optional

from a2a.server.events.event_queue import EventQueue
from a2a.server.tasks import TaskStore
from a2a.types import Task, TaskState, TaskStatusUpdateEvent, TaskArtifactUpdateEvent
from a2a.server.agent_execution import RequestContext
from a2a.server.agent_execution import AgentExecutor

from kagent.core.a2a._requests import KAgentUser
from kagent.core.a2a import get_kagent_metadata_key

logger = logging.getLogger(__name__)


class PersistingEventQueue(EventQueue):
    """An EventQueue that persists task updates to the TaskStore."""

    def __init__(self, task_store: TaskStore, task: Task):
        self.task_store = task_store
        self.task = task
        self.queue = asyncio.Queue()

    async def enqueue_event(self, event) -> None:
        """Enqueue an event and persist changes to the task store."""
        # Process the event to update the task object
        if isinstance(event, TaskStatusUpdateEvent):
            self.task.status = event.status
            # Also update task message if present in status
            # But status has message.
            try:
                await self.task_store.save(self.task)
            except Exception as e:
                logger.error(f"Failed to save task {self.task.id}: {e}", exc_info=True)

        elif isinstance(event, TaskArtifactUpdateEvent):
            # This is tricky because Artifacts are usually managed separately or embedded in Task?
            # A2A Task has `artifacts` field.
            if self.task.artifacts is None:
                self.task.artifacts = []

            # Find existing artifact or add new one
            found = False
            for i, artifact in enumerate(self.task.artifacts):
                if artifact.artifact_id == event.artifact.artifact_id:
                    self.task.artifacts[i] = event.artifact
                    found = True
                    break

            if not found:
                self.task.artifacts.append(event.artifact)

            try:
                await self.task_store.save(self.task)
            except Exception as e:
                logger.error(f"Failed to save task {self.task.id} (artifact update): {e}", exc_info=True)

        # For debugging/logging, we can put it in the queue
        await self.queue.put(event)


async def resume_tasks(
    task_store: TaskStore,
    agent_executor: AgentExecutor,
):
    """Resume execution of tasks that were interrupted."""
    try:
        # Check if list method exists (duck typing)
        if not hasattr(task_store, "list"):
            logger.warning("TaskStore does not support listing tasks. Skipping resumption.")
            return

        # List tasks in "working" state
        logger.info("Checking for tasks to resume...")
        working_tasks = await task_store.list(state=TaskState.working)
        logger.info(f"Found {len(working_tasks)} working tasks to resume.")

        # Also check for input_required tasks?
        # If input is required, the agent waits. Resuming usually implies restarting the wait loop or checking if input arrived.
        # But for now, focus on "working" tasks (crashed).

        for task in working_tasks:
            logger.info(f"Resuming task {task.id}")
            asyncio.create_task(_run_task(task_store, agent_executor, task))

    except Exception as e:
        logger.error(f"Failed to resume tasks: {e}", exc_info=True)


async def _run_task(
    task_store: TaskStore,
    agent_executor: AgentExecutor,
    task: Task,
):
    """Run a single task."""
    try:
        user_id = "unknown"
        user_id_key = get_kagent_metadata_key("user_id")

        if task.status and task.status.metadata:
            user_id = task.status.metadata.get(user_id_key) or task.status.metadata.get("user_id") or "unknown"

        if user_id == "unknown" and task.metadata:
            user_id = task.metadata.get(user_id_key) or task.metadata.get("user_id") or "unknown"

        context = RequestContext(
            task_id=task.id,
            context_id=task.context_id,
            message=task.input,
            current_task=task,
            user=KAgentUser(user_id=user_id) if user_id != "unknown" else None,
        )

        # Create PersistingEventQueue
        event_queue = PersistingEventQueue(task_store, task)

        # Execute
        await agent_executor.execute(context, event_queue)

        logger.info(f"Task {task.id} resumed and completed.")

    except Exception as e:
        logger.error(f"Error resuming task {task.id}: {e}", exc_info=True)
        # Mark as failed if resumption fails?
        # Maybe.
