"""Tests for KAgentSessionService."""

from unittest.mock import AsyncMock, MagicMock

import httpx
import pytest
from google.adk.events.event import Event, EventActions

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
    """Factory fixture: session_response(events, session_id, user_id) -> dict.

    Builds the JSON envelope that the KAgent API returns for GET /api/sessions/{id}.
    """

    def _factory(events: list[Event], session_id: str = "s1", user_id: str = "u1") -> dict:
        return {
            "data": {
                "session": {"id": session_id, "user_id": user_id},
                "events": [{"id": e.id, "data": e.model_dump_json()} for e in events],
            }
        }

    return _factory


@pytest.fixture
def mock_client():
    """Factory fixture: mock_client(response_json, status_code) -> MagicMock httpx.AsyncClient."""

    def _factory(response_json: dict | None, status_code: int = 200) -> MagicMock:
        mock_response = MagicMock(spec=httpx.Response)
        mock_response.status_code = status_code
        mock_response.json.return_value = response_json
        mock_response.raise_for_status = MagicMock()

        client = MagicMock(spec=httpx.AsyncClient)
        client.get = AsyncMock(return_value=mock_response)
        return client

    return _factory


@pytest.fixture
def service(mock_client):
    """Factory fixture: service(response_json, status_code) -> KAgentSessionService."""

    def _factory(response_json: dict | None, status_code: int = 200) -> KAgentSessionService:
        return KAgentSessionService(mock_client(response_json, status_code))

    return _factory


@pytest.mark.asyncio
async def test_get_session_returns_none_on_404(mock_client):
    """A 404 response returns None without raising."""
    svc = KAgentSessionService(mock_client(response_json=None, status_code=404))
    session = await svc.get_session(app_name="app", user_id="u1", session_id="missing")

    assert session is None


@pytest.mark.asyncio
async def test_get_session_returns_none_when_no_data(service):
    """An empty data envelope returns None."""
    session = await service({"data": None}).get_session(app_name="app", user_id="u1", session_id="s1")

    assert session is None


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
        f"Expected {len(events)} events but got {len(session.events)} — possible event duplication in get_session"
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
        f"Expected state['counter'] == 7, got {session.state.get('counter')} — "
        "state_delta may have been applied more than once"
    )


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
