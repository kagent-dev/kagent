"""Request headers reach a turn in full but are persisted without their credentials."""

from unittest.mock import AsyncMock, MagicMock

import pytest
from a2a.auth.user import User
from a2a.server.agent_execution.context import RequestContext
from a2a.server.context import ServerCallContext
from a2a.types import Message, Part, Role, SendMessageRequest, TaskState
from google.adk.agents import BaseAgent
from google.adk.events import Event, EventActions
from google.adk.runners import Runner
from google.adk.sessions import DatabaseSessionService
from google.adk.sessions.schemas.v1 import StorageMetadata
from google.genai import types

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._local_session_service import (
    LocalSessionService,
    event_data_without_credential_headers,
    state_without_credential_headers,
)
from kagent.adk._request_headers import (
    headers_state_delta,
    is_credential_header,
    request_headers,
    without_credentials,
)

APP_NAME = "test-app"
MARKER = "Bearer marker-0f3c2a"


@pytest.mark.parametrize(
    "name",
    ["authorization", "Authorization", "proxy-authorization", "cookie", "x-api-key", "x-goog-api-key", "x-auth-token"],
)
def test_credential_headers(name):
    assert is_credential_header(name)


@pytest.mark.parametrize("name", ["x-user-id", "x-kagent-source", "x-kagent-root-context-id", "content-type"])
def test_other_headers(name):
    assert not is_credential_header(name)


def test_state_delta_keeps_credentials_in_temp_state_only():
    headers = {"authorization": MARKER, "x-user-id": "alice"}

    delta = headers_state_delta(headers)

    assert delta == {"headers": {"x-user-id": "alice"}, "temp:headers": headers}
    assert request_headers(delta) == headers


def test_request_headers_falls_back_to_persisted_headers():
    assert request_headers({"headers": {"x-user-id": "alice"}}) == {"x-user-id": "alice"}
    assert request_headers({}) == {}


def test_without_credentials():
    assert without_credentials({"Authorization": MARKER, "Cookie": "c", "x-user-id": "alice"}) == {"x-user-id": "alice"}


def test_cleaning_leaves_rows_without_credentials_alone():
    assert state_without_credential_headers({"headers": {"x-user-id": "alice"}}) is None
    assert state_without_credential_headers({"session_name": "s"}) is None
    assert event_data_without_credential_headers({"actions": {"state_delta": {"other": 1}}}) is None
    assert event_data_without_credential_headers({"author": "user"}) is None


def test_cleaning_removes_credentials_from_state_and_event():
    assert state_without_credential_headers({"headers": {"authorization": MARKER, "x-user-id": "a"}, "k": 1}) == {
        "headers": {"x-user-id": "a"},
        "k": 1,
    }
    event = {"id": "e", "actions": {"state_delta": {"headers": {"authorization": MARKER}}, "skip": False}}
    assert event_data_without_credential_headers(event) == {
        "id": "e",
        "actions": {"state_delta": {"headers": {}}, "skip": False},
    }


async def test_a_turn_reads_the_credential_but_the_database_never_holds_it(tmp_path):
    seen = []

    class HeaderReadingAgent(BaseAgent):
        async def _run_async_impl(self, ctx):
            seen.append(request_headers(ctx.session.state).get("authorization"))
            yield Event(author=self.name, content=types.Content(role="model", parts=[types.Part(text="ok")]))

    path = tmp_path / "sessions.db"
    service = LocalSessionService(db_url=f"sqlite+aiosqlite:///{path}")
    runner = Runner(app_name=APP_NAME, agent=HeaderReadingAgent(name="reader"), session_service=service)
    request = RequestContext(
        ServerCallContext(
            user=MagicMock(spec=User, user_name="alice"),
            state={"headers": {"authorization": MARKER, "x-user-id": "alice"}},
        ),
        SendMessageRequest(message=Message(message_id="m", role=Role.ROLE_USER, parts=[Part(text="hi")])),
        task_id="task",
        context_id="context",
    )
    queue = AsyncMock()
    try:
        await A2aAgentExecutor(runner=lambda: runner).execute(request, queue)
        assert queue.enqueue_event.await_args_list[-1].args[0].status.state == TaskState.TASK_STATE_COMPLETED
        session = await service.get_session(app_name=APP_NAME, user_id="alice", session_id="context")
    finally:
        await service.close()

    assert seen == [MARKER]
    assert session.state["headers"] == {"x-user-id": "alice"}
    assert "temp:headers" not in session.state
    assert MARKER.encode() not in path.read_bytes()


async def test_stored_credentials_are_removed_once(tmp_path):
    path = tmp_path / "sessions.db"
    url = f"sqlite+aiosqlite:///{path}"
    headers = {"authorization": MARKER, "x-user-id": "alice"}

    # A database written before the credentials were kept out of it.
    legacy = DatabaseSessionService(db_url=url)
    try:
        session = await legacy.create_session(
            app_name=APP_NAME, user_id="conversation", session_id="conversation", state={"headers": headers}
        )
        for turn in range(3):
            await legacy.append_event(
                session,
                Event(
                    author="user",
                    invocation_id=f"turn-{turn}",
                    content=types.Content(role="user", parts=[types.Part(text="hi")]),
                    actions=EventActions(state_delta={"headers": headers}),
                ),
            )
    finally:
        await legacy.close()
    assert MARKER.encode() in path.read_bytes()

    service = LocalSessionService(db_url=url)
    try:
        session = await service.get_session(app_name=APP_NAME, user_id="anyone", session_id="anything")
    finally:
        await service.close()

    assert session.state["headers"] == {"x-user-id": "alice"}
    assert [event.actions.state_delta for event in session.events] == [{"headers": {"x-user-id": "alice"}}] * 3
    assert MARKER.encode() not in path.read_bytes()

    # The removal is recorded: a later start does not scan the conversation again.
    service = LocalSessionService(db_url=url)
    try:
        await service.prepare_tables()
        async with service.database_session_factory() as sql_session:
            assert await sql_session.get(StorageMetadata, "kagent_credential_headers_removed") is not None
    finally:
        await service.close()
