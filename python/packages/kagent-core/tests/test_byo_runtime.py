"""Exercise the deployed Python BYO builders over the same sockets as Substrate."""

from importlib import import_module
from importlib.util import find_spec
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock
from uuid import uuid4

import grpc
import httpx
import pytest
from a2a.types import a2a_pb2 as a2a
from a2a.types import a2a_pb2_grpc
from kagent.runtime.v1 import task_store_pb2_grpc
from test_task_store import Storage, send

from kagent.core import KAgentConfig
from kagent.core._runtime_config import RuntimeConfig
from kagent.core.a2a._task_store import KAgentTaskStore


def openai_agent(monkeypatch):
    from agents import Agent, Runner

    def run_streamed(agent, user_input, *, session, context):
        assert user_input == "go"
        assert session is None
        assert context.session_id == "conversation"
        return SimpleNamespace(stream_events=MagicMock(), final_output="OpenAI answer")

    monkeypatch.setattr(Runner, "run_streamed", run_streamed)
    return {"agent": Agent(name="test")}


def langgraph_agent(monkeypatch):
    from langchain_core.messages import AIMessage
    from langgraph.graph import END, START, MessagesState, StateGraph

    graph = StateGraph(MessagesState)
    graph.add_node("reply", lambda state: {"messages": [AIMessage(content="LangGraph answer")]})
    graph.add_edge(START, "reply")
    graph.add_edge("reply", END)
    return {"graph": graph.compile()}


def crewai_agent(monkeypatch):
    return {"crew": SimpleNamespace(kickoff_async=AsyncMock(return_value=SimpleNamespace(raw="CrewAI answer")))}


@pytest.mark.parametrize(
    "adapter,agent,answer",
    [
        ("openai", openai_agent, "OpenAI answer"),
        ("langgraph", langgraph_agent, "LangGraph answer"),
        ("crewai", crewai_agent, "CrewAI answer"),
    ],
)
@pytest.mark.asyncio
async def test_ready_byo_serves_and_persists_message(adapter, agent, answer, monkeypatch, unused_tcp_port, tmp_path):
    if find_spec(f"kagent.{adapter}") is None:
        pytest.skip(f"kagent-{adapter} is not installed")
    module = import_module(f"kagent.{adapter}._a2a")
    session_id = str(uuid4())
    identity = tmp_path / "name"
    identity.write_text(f"session-{session_id}")
    monkeypatch.setattr(module, "KAgentTaskStore", lambda client: KAgentTaskStore(client, identity))

    storage = Storage(session_id)
    controller = grpc.aio.server()
    task_store_pb2_grpc.add_TaskStoreServiceServicer_to_server(storage, controller)
    controller_port = controller.add_insecure_port("127.0.0.1:0")
    await controller.start()
    try:
        config = KAgentConfig(
            api_url=f"http://127.0.0.1:{controller_port}",
            gateway_url="http://localhost",
            namespace="test",
            name="byo",
            runtime=RuntimeConfig(host="127.0.0.1", port=unused_tcp_port),
        )
        app = module.KAgentApp(
            **agent(monkeypatch), config=config, agent_card=a2a.AgentCard(name="byo", version="1"), tracing=False
        ).build()
        async with app.router.lifespan_context(app):
            async with httpx.AsyncClient(trust_env=False) as http:
                assert (await http.get("http://127.0.0.1:8081/readyz")).status_code == 200
            async with grpc.aio.insecure_channel(f"127.0.0.1:{unused_tcp_port}") as channel:
                request = send(str(uuid4()))
                request.message.context_id = "conversation"
                response = await a2a_pb2_grpc.A2AServiceStub(channel).SendMessage(
                    request, metadata=(("x-kagent-dispatch-id", "dispatch"),), timeout=10
                )
        assert response.task.status.state == a2a.TASK_STATE_COMPLETED
        assert storage.task.id == response.task.id
        assert storage.task.status.state == a2a.TASK_STATE_COMPLETED
        assert storage.task.artifacts[0].parts[0].text == answer
        assert storage.settlements == [storage.version]
        assert set(storage.dispatch_ids) == {"dispatch"}
    finally:
        await controller.stop(None)
