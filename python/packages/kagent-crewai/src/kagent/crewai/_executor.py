import logging
import uuid
from datetime import datetime, timezone
from typing import Any, Union, override

import httpx
from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Artifact,
    DataPart,
    Message,
    Part,
    Role,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from pydantic import BaseModel

from crewai import Crew, Flow
from crewai.memory import LongTermMemory
from kagent.core.tracing._span_processor import (
    clear_kagent_span_attributes,
    set_kagent_span_attributes,
)

from ._listeners import A2ACrewAIListener
from ._memory import KagentMemoryStorage
from ._state import KagentFlowPersistence

logger = logging.getLogger(__name__)


class CrewAIAgentExecutorConfig(BaseModel):
    execution_timeout: float = 300.0


class CrewAIAgentExecutor(AgentExecutor):
    def __init__(
        self,
        *,
        crew: Union[Crew, Flow],
        app_name: str,
        config: CrewAIAgentExecutorConfig | None = None,
        http_client: httpx.AsyncClient,
    ):
        super().__init__()
        self._crew = crew
        self.app_name = app_name
        self._config = config or CrewAIAgentExecutorConfig()
        self._http_client = http_client

    @override
    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        raise NotImplementedError("Cancellation is not implemented")

    @override
    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        if not context.message:
            raise ValueError("A2A request must have a message")

        # Convert the a2a request to kagent span attributes.
        span_attributes = _convert_a2a_request_to_span_attributes(context)

        # Set kagent span attributes for all spans in context.
        context_token = set_kagent_span_attributes(span_attributes)
        try:
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
                        "app_name": self.app_name,
                        "session_id": context.context_id,
                    },
                )
            )

            # This listener will capture and convert CrewAI events and enqueue them to A2A event queue
            A2ACrewAIListener(context, event_queue, self.app_name)

            try:
                inputs = None
                if context.message and context.message.parts:
                    for part in context.message.parts:
                        if isinstance(part, DataPart):
                            inputs = part.root.data
                            break
                if inputs is None:
                    user_input = context.get_user_input()
                    inputs = {"input": user_input} if user_input else {}

                session_id = getattr(context, "session_id", context.context_id)
                user_id = getattr(context, "user_id", "admin@kagent.dev")

                if isinstance(self._crew, Flow):
                    flow_class = type(self._crew)
                    persistence = KagentFlowPersistence(
                        thread_id=session_id,
                        user_id=user_id,
                        base_url=str(self._http_client.base_url),
                    )
                    flow_instance = flow_class()
                    flow_instance.persistence = persistence

                    # setting "id" in flow input will enable reusing persisted flow state
                    # if no flow state is persisted or if persistence is not enabled, this works like a normal kickoff
                    inputs["id"] = session_id

                    # output_text will be None if the last method in the flow does not return anything but updates the state instead
                    output_text = await flow_instance.kickoff_async(inputs=inputs)
                    result_text = output_text or flow_instance.state.model_dump_json()
                else:
                    if self._crew.memory:
                        self._crew.long_term_memory = LongTermMemory(
                            KagentMemoryStorage(
                                thread_id=session_id,
                                user_id=user_id,
                                base_url=str(self._http_client.base_url),
                            )
                        )
                    result = await self._crew.kickoff_async(inputs=inputs)
                    result_text = str(result.raw or "No response was generated.")

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
                            timestamp=datetime.now(timezone.utc).isoformat(),
                        ),
                        context_id=context.context_id,
                        final=True,
                    )
                )

            except Exception as e:
                logger.error(f"Error during CrewAI execution: {e}", exc_info=True)
                await event_queue.enqueue_event(
                    TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        status=TaskStatus(
                            state=TaskState.failed,
                            timestamp=datetime.now(timezone.utc).isoformat(),
                            message=Message(
                                message_id=str(uuid.uuid4()),
                                role=Role.agent,
                                parts=[Part(TextPart(text=str(e)))],
                            ),
                        ),
                        context_id=context.context_id,
                        final=True,
                    )
                )
        finally:
            clear_kagent_span_attributes(context_token)


def _get_user_id(request: RequestContext) -> str:
    # Get user from call context if available (auth is enabled on a2a server)
    if request.call_context and request.call_context.user and request.call_context.user.user_name:
        return request.call_context.user.user_name

    # Get user from context id
    return f"A2A_USER_{request.context_id}"


def _convert_a2a_request_to_span_attributes(
    request: RequestContext,
) -> dict[str, Any]:
    if not request.message:
        raise ValueError("Request message cannot be None")

    span_attributes = {
        "kagent.user_id": _get_user_id(request),
        "gen_ai.conversation.id": request.context_id,
    }

    if request.task_id:
        span_attributes["gen_ai.task.id"] = request.task_id

    return span_attributes
