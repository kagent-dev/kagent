from unittest.mock import AsyncMock, MagicMock

import httpx
import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import DataPart, Message, MessageSendParams, Part, Role, TextPart

from kagent.crewai._executor import CrewAIAgentExecutor


def _request_context(*parts: Part) -> RequestContext:
    message = Message(role=Role.user, message_id="msg-1", parts=list(parts))
    return RequestContext(request=MessageSendParams(message=message))


def _make_crew() -> MagicMock:
    crew = MagicMock()
    # Skip the long-term memory branch, which needs a live base URL.
    crew.memory = False
    crew.kickoff_async = AsyncMock(return_value=MagicMock(raw="done"))
    return crew


async def _run(crew: MagicMock, context: RequestContext) -> None:
    executor = CrewAIAgentExecutor(
        crew=crew,
        app_name="test",
        http_client=httpx.AsyncClient(),
    )
    await executor.execute(context, EventQueue())


@pytest.mark.asyncio
async def test_execute_passes_datapart_data_as_inputs():
    crew = _make_crew()
    context = _request_context(Part(DataPart(data={"topic": "ai"})))

    await _run(crew, context)

    crew.kickoff_async.assert_awaited_once_with(inputs={"topic": "ai"})


@pytest.mark.asyncio
async def test_execute_falls_back_to_text_input_without_datapart():
    crew = _make_crew()
    context = _request_context(Part(TextPart(text="hello")))

    await _run(crew, context)

    crew.kickoff_async.assert_awaited_once_with(inputs={"input": "hello"})
