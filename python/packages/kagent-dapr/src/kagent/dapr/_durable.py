import asyncio
import json
import logging
import uuid
from datetime import UTC, datetime
from typing import override

from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    Message,
    Part,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
import dapr.ext.workflow as wf
from dapr_agents import DurableAgent
from kagent.core.tracing._span_processor import (
    clear_kagent_span_attributes,
    set_kagent_span_attributes,
)
logger = logging.getLogger(__name__)


class DaprDurableAgentExecutor(AgentExecutor):
    def __init__(self, *, durable_agent: DurableAgent, app_name: str):
        super().__init__()
        self._durable_agent = durable_agent
        self.app_name = app_name
        self._durable_agent.start()
        self._wf_client = wf.DaprWorkflowClient()

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue) -> None:
        raise NotImplementedError("Cancellation is not implemented for DaprDurableAgentExecutor")

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        if not context.message:
            raise ValueError("A2A request must have a message")

        span_attributes: dict[str, str | None] = _convert_a2a_request_to_span_attributes(context)
        context_token = set_kagent_span_attributes(span_attributes)

        try:
            if type(context.task_id) is not str or type(context.context_id) is not str:
                raise ValueError("A2A request must have string task_id and context_id")
            if not context.current_task:
                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.submitted,
                            message=context.message,
                            timestamp=datetime.now(UTC).isoformat(),
                        ),
                        context_id=context.context_id,
                        final=False,
                    )
                )

            await event_queue.enqueue_event(
                TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    status=TaskStatus(
                        state=TaskState.working,
                        timestamp=datetime.now(UTC).isoformat(),
                    ),
                    context_id=context.context_id,
                    final=False,
                    metadata={
                        "app_name": self.app_name,
                        "session_id": context.context_id,
                    },
                )
            )

            try:
                user_input = context.get_user_input()

                instance_id = self._wf_client.schedule_new_workflow(
                    workflow=self._durable_agent.agent_workflow,
                    input={"task": user_input},
                )

                loop = asyncio.get_running_loop()
                result_state = await loop.run_in_executor(
                    None, self._wf_client.wait_for_workflow_completion, instance_id
                )

                result_text = ""
                if result_state and result_state.serialized_output:
                    try:
                        parsed = json.loads(result_state.serialized_output)
                        if isinstance(parsed, str):
                            result_text = parsed
                        elif isinstance(parsed, dict):
                            result_text = parsed.get("content", parsed.get("result", str(parsed)))
                        else:
                            result_text = str(parsed)
                    except (json.JSONDecodeError, TypeError):
                        result_text = str(result_state.serialized_output)

                if result_text:
                    await event_queue.enqueue_event(
                        TaskArtifactUpdateEvent(
                            task_id=context.task_id,
                            last_chunk=True,
                            context_id=context.context_id,
                            artifact=Artifact(
                                artifact_id=str(uuid.uuid4()),
                                parts=[Part(TextPart(text=result_text))],
                            ),
                        )
                    )

                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.completed,
                            timestamp=datetime.now(UTC).isoformat(),
                        ),
                        context_id=context.context_id,
                        final=True,
                    )
                )

            except Exception as e:
                logger.error(f"Error during DurableAgent execution: {e}", exc_info=True)
                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.failed,
                            timestamp=datetime.now(UTC).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text=f"DurableAgent execution failed: {e}"))],
                            ),
                        ),
                        context_id=context.context_id,
                        final=True,
                    )
                )

        finally:
            clear_kagent_span_attributes(context_token)


def _get_user_id(request: RequestContext) -> str:
    if request.call_context and request.call_context.user and request.call_context.user.user_name:
        return request.call_context.user.user_name

    return f"A2A_USER_{request.context_id}"


def _convert_a2a_request_to_span_attributes(
    request: RequestContext,
) -> dict[str, str | None]:
    if not request.message:
        raise ValueError("Request message cannot be None")

    span_attributes: dict[str, str | None] = {
        "kagent.user_id": _get_user_id(request),
        "gen_ai.conversation.id": request.context_id,
    }

    if request.task_id:
        span_attributes["gen_ai.task.id"] = request.task_id

    return span_attributes
