from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import DataPart, Message, MessageSendParams, Part, Role, TextPart
from crewai import Flow
from crewai.flow.flow import FlowState, start

from kagent.crewai._executor import CrewAIAgentExecutor
from kagent.crewai._state import KagentFlowPersistence


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
        controller_client=MagicMock(),
    )
    with patch("kagent.crewai._executor.A2ACrewAIListener"):
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


class _RestoredState(FlowState):
    value: str = "fresh"


class _RestoredFlow(Flow[_RestoredState]):
    @start()
    def result(self) -> str:
        return self.state.value


@pytest.mark.asyncio
async def test_execute_restores_and_saves_flow_state_through_controller(monkeypatch):
    persistence = MagicMock()
    persistence.load_state.return_value = {"id": "thread-1", "value": "restored"}
    persistence.flush = AsyncMock()
    create = AsyncMock(return_value=persistence)
    monkeypatch.setattr(KagentFlowPersistence, "create", create)
    monkeypatch.setattr("kagent.crewai._executor.A2ACrewAIListener", MagicMock())
    context = RequestContext(
        request=MessageSendParams(
            message=Message(
                role=Role.user,
                message_id="msg-1",
                context_id="thread-1",
                parts=[Part(TextPart(text="hello"))],
            )
        )
    )
    controller_client = MagicMock()
    executor = CrewAIAgentExecutor(
        crew=_RestoredFlow(),
        app_name="test",
        controller_client=controller_client,
    )

    await executor.execute(context, EventQueue())

    create.assert_awaited_once_with("thread-1", "admin@kagent.dev", controller_client)
    persistence.load_state.assert_called_once_with("thread-1")
    persistence.save_state.assert_called_once()
    saved_flow_id, saved_method, saved_state = persistence.save_state.call_args.args
    assert saved_flow_id == "thread-1"
    assert saved_method == "kickoff"
    assert saved_state.value == "restored"
    persistence.flush.assert_awaited_once()
