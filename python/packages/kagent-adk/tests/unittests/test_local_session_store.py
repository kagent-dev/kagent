"""Tests for durable-dir and in-memory ADK session storage."""

from a2a.types import AgentCard
from google.protobuf.json_format import ParseDict

import kagent.adk._a2a as _a2a
from kagent.adk import KAgentApp
from kagent.adk.types import AgentConfig, Gemini

APP_NAME = "test-app"


def make_kagent_app(agent_config: AgentConfig | None = None) -> KAgentApp:
    card = ParseDict(
        {
            "name": APP_NAME,
            "description": "test agent",
            "version": "0.0.1",
            "supportedInterfaces": [{"url": "http://localhost:8080", "protocolBinding": "JSONRPC"}],
            "capabilities": {},
            "defaultInputModes": ["text/plain"],
            "defaultOutputModes": ["text/plain"],
            "skills": [],
        },
        AgentCard(),
    )
    # root_agent_factory is only invoked per-request by the executor, never during build.
    return KAgentApp(
        root_agent_factory=lambda: None,
        agent_card=card,
        kagent_api_url="http://localhost:8083",
        app_name=APP_NAME,
        agent_config=agent_config,
    )


def config_with_session_db_url(url: str | None) -> AgentConfig:
    return AgentConfig(
        model=Gemini(type="gemini", model="gemini-2.5-flash"),
        description="d",
        instruction="i",
        session_db_url=url,
    )


def test_config_session_db_url_selects_local_store(monkeypatch):
    constructed = {}

    class FakeDatabaseSessionService:
        def __init__(self, db_url):
            constructed["db_url"] = db_url

    monkeypatch.setattr(_a2a, "LocalSessionService", FakeDatabaseSessionService)
    make_kagent_app(config_with_session_db_url("sqlite+aiosqlite:////data/sessions.db")).build()

    assert constructed == {"db_url": "sqlite+aiosqlite:////data/sessions.db"}


def test_no_url_keeps_in_memory_sessions(monkeypatch):
    def boom(*args, **kwargs):
        raise AssertionError("DatabaseSessionService must not be constructed without a session DB URL")

    monkeypatch.setattr(_a2a, "LocalSessionService", boom)
    make_kagent_app(config_with_session_db_url(None)).build()
    make_kagent_app(None).build()


async def test_snapshot_fork_uses_native_history_with_new_public_context(tmp_path):
    import shutil
    from unittest.mock import AsyncMock, MagicMock

    from a2a.auth.user import User
    from a2a.server.agent_execution.context import RequestContext
    from a2a.server.context import ServerCallContext
    from a2a.types import Message, Part, Role, SendMessageRequest, TaskState
    from google.adk.agents import BaseAgent
    from google.adk.events import Event
    from google.adk.runners import Runner
    from google.genai import types

    from kagent.adk._agent_executor import A2aAgentExecutor
    from kagent.adk._local_session_service import LocalSessionService
    from kagent.adk._request_identity import public_context_id, request_user_id

    counts = []

    class RememberingAgent(BaseAgent):
        async def _run_async_impl(self, ctx):
            counts.append(len(ctx.session.events))
            yield Event(author=self.name, content=types.Content(role="model", parts=[types.Part(text="remembered")]))

    async def send(path, context_id, user_id):
        service = LocalSessionService(db_url=f"sqlite+aiosqlite:///{path}")
        runner = Runner(app_name=APP_NAME, agent=RememberingAgent(name="memory"), session_service=service)
        request = RequestContext(
            ServerCallContext(user=MagicMock(spec=User, user_name=user_id)),
            SendMessageRequest(
                message=Message(message_id=f"message-{len(counts)}", role=Role.ROLE_USER, parts=[Part(text="continue")])
            ),
            task_id=f"task-{len(counts)}",
            context_id=context_id,
        )
        queue = AsyncMock()
        executor = A2aAgentExecutor(runner=lambda: runner)
        try:
            await executor.execute(request, queue)
            events = [call.args[0] for call in queue.enqueue_event.await_args_list]
            assert events[-1].status.state == TaskState.TASK_STATE_COMPLETED
            assert all(event.context_id == context_id for event in events)
            assert public_context_id.get() == ""
            assert request_user_id.get() == ""
        finally:
            await service.close()

    source, fork = tmp_path / "source.db", tmp_path / "fork.db"
    await send(source, "source-context", "alice")
    shutil.copyfile(source, fork)
    await send(source, "source-context", "alice")
    await send(fork, "fork-context", "alice")
    await send(fork, "fork-context", "share-visitor")
    assert counts == [1, 3, 3, 5]
