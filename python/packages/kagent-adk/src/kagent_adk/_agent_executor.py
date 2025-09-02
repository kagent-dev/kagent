from __future__ import annotations

import asyncio
import inspect
import logging
import uuid
from datetime import datetime, timezone
from typing import Any, Awaitable, Callable, Optional

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    Message,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from google.adk.a2a.converters.event_converter import convert_event_to_a2a_events
from google.adk.a2a.converters.request_converter import convert_a2a_request_to_adk_run_args
from google.adk.a2a.converters.utils import _get_adk_metadata_key
from google.adk.a2a.executor.task_result_aggregator import TaskResultAggregator
from google.adk.runners import Runner
from opentelemetry import trace
from pydantic import BaseModel
from typing_extensions import override

logger = logging.getLogger("google_adk." + __name__)


class A2aAgentExecutorConfig(BaseModel):
    """Configuration for the A2aAgentExecutor."""

    pass


# This class is a copy of the A2aAgentExecutor class in the ADK sdk,
# with the following changes:
# - The runner is ALWAYS a callable that returns a Runner instance
# - The runner is cleaned up at the end of the execution
class A2aAgentExecutor(AgentExecutor):
    """An AgentExecutor that runs an ADK Agent against an A2A request and
    publishes updates to an event queue.
    """

    def __init__(
        self,
        *,
        runner: Callable[..., Runner | Awaitable[Runner]],
        config: Optional[A2aAgentExecutorConfig] = None,
    ):
        super().__init__()
        self._runner = runner
        self._config = config

    async def _resolve_runner(self) -> Runner:
        """Resolve the runner, handling cases where it's a callable that returns a Runner."""
        if callable(self._runner):
            # Call the function to get the runner
            result = self._runner()

            # Handle async callables
            if inspect.iscoroutine(result):
                resolved_runner = await result
            else:
                resolved_runner = result

            # Ensure we got a Runner instance
            if not isinstance(resolved_runner, Runner):
                raise TypeError(f"Callable must return a Runner instance, got {type(resolved_runner)}")

            return resolved_runner

        raise TypeError(
            f"Runner must be a Runner instance or a callable that returns a Runner, got {type(self._runner)}"
        )

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        """Cancel the execution."""
        # TODO: Implement proper cancellation logic if needed
        raise NotImplementedError("Cancellation is not supported")

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        """Executes an A2A request and publishes updates to the event queue
        specified. It runs as following:
        * Takes the input from the A2A request
        * Convert the input to ADK input content, and runs the ADK agent
        * Collects output events of the underlying ADK Agent
        * Converts the ADK output events into A2A task updates
        * Publishes the updates back to A2A server via event queue
        """
        if not context.message:
            raise ValueError("A2A request must have a message")

        # for new task, create a task submitted event
        if not context.current_task:
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.submitted,
                        message=context.message,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                    ),
                    context_id=context.context_id,
                    final=False,
                )
            )

        # Handle the request and publish updates to the event queue
        runner = await self._resolve_runner()
        try:
            await self._handle_request(context, event_queue, runner)
        except Exception as e:
            logger.error("Error handling A2A request: %s", e, exc_info=True)
            # Publish failure event
            try:
                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.failed,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[TextPart(text=str(e))],
                            ),
                        ),
                        context_id=context.context_id,
                        final=True,
                    )
                )
            except Exception as enqueue_error:
                logger.error("Failed to publish failure event: %s", enqueue_error, exc_info=True)
        finally:
            # Shield cleanup from external cancellation so toolsets (e.g., MCP) can
            # gracefully close their sessions without being torn down mid-flight.
            try:
                await asyncio.wait_for(asyncio.shield(runner.close()), timeout=15.0)
            except asyncio.CancelledError:
                # Suppress cancellation during cleanup to avoid noisy tracebacks
                # from libraries that assume non-cancelled close semantics.
                logger.warning("Runner.close() was cancelled; suppressing during cleanup")
            except Exception as close_error:
                logger.error("Error during runner.close(): %s", close_error, exc_info=True)

    async def _handle_request(
        self,
        context: RequestContext,
        event_queue: EventQueue,
        runner: Runner,
    ):
        # Convert the a2a request to ADK run args
        run_args = convert_a2a_request_to_adk_run_args(context)

        # ensure the session exists
        session = await self._prepare_session(context, run_args, runner)

        current_span = trace.get_current_span()
        if run_args["user_id"]:
            current_span.set_attribute("kagent.user_id", run_args["user_id"])
        if context.task_id:
            current_span.set_attribute("gen_ai.task.id", context.task_id)
        if run_args["session_id"]:
            current_span.set_attribute("gen_ai.converstation.id", run_args["session_id"])

        # create invocation context
        invocation_context = runner._new_invocation_context(
            session=session,
            new_message=run_args["new_message"],
            run_config=run_args["run_config"],
        )

        # publish the task working event
        await event_queue.enqueue_event(
            TaskStatusUpdateEvent(
                task_id=context.task_id,
                status=TaskStatus(
                    state=TaskState.working,
                    timestamp=datetime.now(timezone.utc).isoformat(),
                ),
                context_id=context.context_id,
                final=False,
                metadata={
                    _get_adk_metadata_key("app_name"): runner.app_name,
                    _get_adk_metadata_key("user_id"): run_args["user_id"],
                    _get_adk_metadata_key("session_id"): run_args["session_id"],
                },
            )
        )

        task_result_aggregator = TaskResultAggregator()
        async for adk_event in runner.run_async(**run_args):
            for a2a_event in convert_event_to_a2a_events(
                adk_event, invocation_context, context.task_id, context.context_id
            ):
                task_result_aggregator.process_event(a2a_event)
                await event_queue.enqueue_event(a2a_event)

        # publish the task result event - this is final
        if (
            task_result_aggregator.task_state == TaskState.working
            and task_result_aggregator.task_status_message is not None
            and task_result_aggregator.task_status_message.parts
        ):
            # if task is still working properly, publish the artifact update event as
            # the final result according to a2a protocol.
            await event_queue.enqueue_event(
                TaskArtifactUpdateEvent(
                    task_id=context.task_id,
                    last_chunk=True,
                    context_id=context.context_id,
                    artifact=Artifact(
                        artifact_id=str(uuid.uuid4()),
                        parts=task_result_aggregator.task_status_message.parts,
                    ),
                )
            )
            # public the final status update event
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.completed,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )
        else:
            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=task_result_aggregator.task_state,
                        timestamp=datetime.now(timezone.utc).isoformat(),
                        message=task_result_aggregator.task_status_message,
                    ),
                    context_id=context.context_id,
                    final=True,
                )
            )

    async def _prepare_session(self, context: RequestContext, run_args: dict[str, Any], runner: Runner):
        session_id = run_args["session_id"]
        # create a new session if not exists
        user_id = run_args["user_id"]
        session = await runner.session_service.get_session(
            app_name=runner.app_name,
            user_id=user_id,
            session_id=session_id,
        )
        if session is None:
            session = await runner.session_service.create_session(
                app_name=runner.app_name,
                user_id=user_id,
                state={},
                session_id=session_id,
            )
            # Update run_args with the new session_id
            run_args["session_id"] = session.id

        return session
