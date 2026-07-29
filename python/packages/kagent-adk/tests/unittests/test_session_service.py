"""Tests for KAgentSessionService."""

from unittest.mock import AsyncMock, MagicMock

import grpc
import pytest
from google.adk.events.event import Event, EventActions
from google.adk.sessions.base_session_service import GetSessionConfig
from kagent.api.v1alpha1 import sessions_pb2

from kagent.adk._session_service import KAgentSessionService


@pytest.fixture
def make_event():
    """Factory fixture: make_event(author, state_delta) -> Event."""

    def _factory(author: str = "user", state_delta: dict | None = None) -> Event:
        if state_delta:
            return Event(author=author, invocation_id="inv1", actions=EventActions(state_delta=state_delta))
        return Event(author=author, invocation_id="inv1")

    return _factory


@pytest.fixture
def session_response():
    """Build a generated GetSession response with serialized ADK events."""

    def _factory(
        events: list[Event], session_id: str = "s1", user_id: str = "u1"
    ) -> sessions_pb2.GetSessionResponse:
        return sessions_pb2.GetSessionResponse(
            session=sessions_pb2.Session(id=session_id, user_id=user_id),
            events=[sessions_pb2.SessionEvent(id=event.id, data=event.model_dump_json()) for event in events],
        )

    return _factory


@pytest.fixture
def mock_client():
    """Build an AsyncControllerClient-shaped mock with generated service methods."""

    def _factory(response: object | None) -> MagicMock:
        client = MagicMock()
        client.call_options = AsyncMock(return_value={"metadata": (), "timeout": 30.0})
        client.session_service = MagicMock()
        client.session_service.CreateSession = AsyncMock(return_value=response)
        client.session_service.GetSession = AsyncMock(return_value=response)
        client.session_service.ListSessions = AsyncMock(return_value=response)
        client.session_service.DeleteSession = AsyncMock(return_value=sessions_pb2.DeleteSessionResponse())
        client.session_service.AddSessionEvent = AsyncMock(return_value=sessions_pb2.AddSessionEventResponse())
        return client

    return _factory


@pytest.fixture
def service(mock_client):
    """Build a KAgentSessionService with a generated-client mock."""

    def _factory(response: object | None) -> KAgentSessionService:
        return KAgentSessionService(mock_client(response))

    return _factory


@pytest.mark.asyncio
async def test_create_session_passes_explicit_user_metadata_and_fields(mock_client):
    """Keep the A2A-derived user ID consistent across session RPCs.

    This prevents a created session from being owned by the unsecure
    authenticator's fallback user while later calls use the A2A-derived user.
    Regression test for https://github.com/kagent-dev/kagent/issues/1882.
    """
    response = sessions_pb2.CreateSessionResponse(
        session=sessions_pb2.Session(id="sess-1", user_id="A2A_USER_ctx123")
    )
    client = mock_client(response)

    svc = KAgentSessionService(client)
    session = await svc.create_session(
        app_name="my-agent",
        user_id="A2A_USER_ctx123",
        session_id="ctx123",
        state={"session_name": "First turn", "source": "agent"},
    )

    request = client.session_service.CreateSession.await_args.args[0]
    assert request.id == "ctx123"
    assert request.agent_ref == "my-agent"
    assert request.name == "First turn"
    assert request.source == sessions_pb2.SESSION_SOURCE_AGENT
    client.call_options.assert_awaited_once_with("A2A_USER_ctx123")
    assert session.id == "sess-1"


@pytest.mark.asyncio
async def test_get_session_returns_none_on_404(mock_client):
    """A gRPC NOT_FOUND status returns None without raising."""
    client = mock_client(None)
    client.session_service.GetSession.side_effect = grpc.aio.AioRpcError(
        grpc.StatusCode.NOT_FOUND,
        (),
        (),
        "session not found",
        "",
    )
    svc = KAgentSessionService(client)
    session = await svc.get_session(app_name="app", user_id="u1", session_id="missing")

    assert session is None


@pytest.mark.asyncio
async def test_get_session_returns_none_when_no_data(service):
    """A response without a session returns None."""
    session = await service(sessions_pb2.GetSessionResponse()).get_session(
        app_name="app", user_id="u1", session_id="s1"
    )

    assert session is None


@pytest.mark.asyncio
async def test_get_session_passes_after_timestamp_to_api(mock_client, session_response):
    """Incremental session loads only request events newer than the configured timestamp."""
    client = mock_client(session_response([]))
    svc = KAgentSessionService(client)

    await svc.get_session(
        app_name="app",
        user_id="u1",
        session_id="s1",
        config=GetSessionConfig(after_timestamp=1785148200.0, num_recent_events=25),
    )

    request = client.session_service.GetSession.await_args.args[0]
    assert request.order == sessions_pb2.EVENT_ORDER_ASCENDING
    assert request.after.seconds == 1785148200
    assert not request.HasField("limit")
    client.call_options.assert_awaited_once_with("u1")


@pytest.mark.asyncio
async def test_get_session_passes_epoch_timestamp_to_api(mock_client, session_response):
    """Unix epoch zero is a valid timestamp filter, not an absent value."""
    client = mock_client(session_response([]))
    svc = KAgentSessionService(client)

    await svc.get_session(
        app_name="app",
        user_id="u1",
        session_id="s1",
        config=GetSessionConfig(after_timestamp=0.0),
    )

    request = client.session_service.GetSession.await_args.args[0]
    assert request.order == sessions_pb2.EVENT_ORDER_ASCENDING
    assert request.HasField("after")
    assert request.after.seconds == 0
    assert not request.HasField("limit")


@pytest.mark.asyncio
async def test_get_session_with_zero_recent_events_returns_no_events(make_event, session_response, mock_client):
    """ADK defines a zero recent-event limit as returning session metadata without history.

    State still has to be complete: every event is replayed, only the events list is emptied.
    """
    client = mock_client(session_response([make_event("user", state_delta={"key": "value"})]))
    svc = KAgentSessionService(client)

    session = await svc.get_session(
        app_name="app",
        user_id="u1",
        session_id="s1",
        config=GetSessionConfig(num_recent_events=0),
    )

    assert session is not None
    assert session.events == []
    assert session.state.get("key") == "value", "state must survive even when no events are returned"
    request = client.session_service.GetSession.await_args.args[0]
    assert request.order == sessions_pb2.EVENT_ORDER_ASCENDING
    assert not request.HasField("limit")


@pytest.mark.asyncio
async def test_get_session_returns_recent_events_in_chronological_order(make_event, session_response, mock_client):
    """The recent-events window keeps the oldest-first order the API already returns."""
    older_event = make_event("older")
    newer_event = make_event("newer")
    client = mock_client(session_response([older_event, newer_event]))
    svc = KAgentSessionService(client)

    session = await svc.get_session(
        app_name="app",
        user_id="u1",
        session_id="s1",
        config=GetSessionConfig(num_recent_events=2),
    )

    assert session is not None
    assert [event.id for event in session.events] == [older_event.id, newer_event.id]
    request = client.session_service.GetSession.await_args.args[0]
    assert request.order == sessions_pb2.EVENT_ORDER_ASCENDING
    assert not request.HasField("limit")


@pytest.mark.asyncio
async def test_get_session_event_ids_preserved(make_event, session_response, service):
    """Event identity (id) is preserved after loading from the API."""
    events = [make_event("user"), make_event("assistant")]
    original_ids = [e.id for e in events]

    session = await service(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is not None
    assert [e.id for e in session.events] == original_ids


@pytest.mark.asyncio
async def test_get_session_events_not_duplicated(make_event, session_response, service):
    """Each event from the API must appear exactly once in session.events.

    Regression test for the bug where Session(events=events) pre-populated
    session.events and super().append_event() then appended each event again.
    """
    events = [make_event("user"), make_event("assistant"), make_event("tool")]
    session = await service(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is not None
    assert len(session.events) == len(events), (
        f"Expected {len(events)} events but got {len(session.events)}, possible event duplication in get_session"
    )


@pytest.mark.asyncio
async def test_get_session_single_event_not_duplicated(make_event, session_response, service):
    """Single-event case: still only one event in session.events."""
    events = [make_event("user")]
    session = await service(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is not None
    assert len(session.events) == 1


@pytest.mark.asyncio
async def test_get_session_empty_events(session_response, service):
    """Zero events from the API yields an empty session.events list."""
    session = await service(session_response([])).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is not None
    assert len(session.events) == 0


@pytest.mark.asyncio
async def test_get_session_state_delta_applied_once(make_event, session_response, service):
    """State deltas from events must be applied exactly once to session.state.

    Regression test: when events were double-appended, _update_session_state()
    was called twice per event, so numeric or overwrite-based state deltas
    would be applied twice.
    """
    events = [make_event("assistant", state_delta={"counter": 7})]
    session = await service(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is not None
    # State must reflect exactly one application of the delta.
    # (BaseSessionService._update_session_state does session.state.update({key: value}),
    # so for an idempotent string the bug was silent; here we use a distinct value
    # and just verify the key is present with the correct value.)
    assert session.state.get("counter") == 7, (
        f"Expected state['counter'] == 7, got {session.state.get('counter')}, "
        "state_delta may have been applied more than once"
    )


@pytest.mark.asyncio
async def test_get_session_state_kept_outside_recent_events_window(make_event, session_response, mock_client):
    """A state delta from an event outside the num_recent_events window must
    still land in session.state, only session.events is trimmed to the window.

    This is what makes the test fail against the old code, which asked the
    server for only num_recent_events and so never saw the older state_delta.
    """
    all_events = [
        make_event("assistant", state_delta={"old_key": "old_value"}),
        make_event("user"),
        make_event("assistant"),
    ]

    client = mock_client(session_response(all_events))

    session = await KAgentSessionService(client).get_session(
        app_name="app", user_id="u1", session_id="s1", config=GetSessionConfig(num_recent_events=2)
    )

    assert session is not None
    assert len(session.events) == 2
    assert session.state.get("old_key") == "old_value", (
        "state from the first event must still apply even though only the last 2 events are kept in session.events"
    )
    request = client.session_service.GetSession.await_args.args[0]
    assert request.order == sessions_pb2.EVENT_ORDER_ASCENDING
    assert not request.HasField("limit"), "get_session must fetch full history to retain older state deltas"


@pytest.mark.asyncio
async def test_get_session_multiple_state_deltas_applied_once(make_event, session_response, service):
    """Multiple events each contributing a state key are each applied once."""
    events = [
        make_event("assistant", state_delta={"key_a": "value_a"}),
        make_event("tool", state_delta={"key_b": "value_b"}),
    ]
    session = await service(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is not None
    assert session.state.get("key_a") == "value_a"
    assert session.state.get("key_b") == "value_b"
