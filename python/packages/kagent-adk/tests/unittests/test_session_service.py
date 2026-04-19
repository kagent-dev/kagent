from unittest import mock
from unittest.mock import AsyncMock, MagicMock

import httpx
import pytest
from google.adk.events.event import Event, EventActions
from google.adk.sessions import Session

from kagent.adk._session_service import KAgentSessionService


# ---------------------------------------------------------------------------
# Shared fixtures for append_event / _recreate_session tests
# ---------------------------------------------------------------------------

@pytest.fixture
def mock_client():
    """Simple AsyncMock client for sequential multi-call tests."""
    return mock.AsyncMock(spec=httpx.AsyncClient)


@pytest.fixture
def session_service(mock_client):
    return KAgentSessionService(client=mock_client)


@pytest.fixture
def sample_session():
    return Session(
        id="test-session-123",
        user_id="test-user",
        app_name="test-app",
        state={"session_name": "Test Session"},
    )


@pytest.fixture
def sample_event():
    return Event(invocation_id="test-invocation", author="user")


# ---------------------------------------------------------------------------
# Fixtures for get_session tests (factory-style, mirrors original test file)
# ---------------------------------------------------------------------------

@pytest.fixture
def make_event():
    """Factory: make_event(author, state_delta) -> Event."""
    def _factory(author: str = "user", state_delta: dict | None = None) -> Event:
        if state_delta:
            return Event(author=author, invocation_id="inv1", actions=EventActions(state_delta=state_delta))
        return Event(author=author, invocation_id="inv1")
    return _factory


@pytest.fixture
def session_response():
    """Factory: build the JSON envelope returned by GET /api/sessions/{id}."""
    def _factory(events: list[Event], session_id: str = "s1", user_id: str = "u1") -> dict:
        return {
            "data": {
                "session": {"id": session_id, "user_id": user_id},
                "events": [{"id": e.id, "data": e.model_dump_json()} for e in events],
            }
        }
    return _factory


@pytest.fixture
def get_client():
    """Factory: get_client(response_json, status_code) -> MagicMock AsyncClient (GET only)."""
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
def get_session_svc(get_client):
    """Factory: get_session_svc(response_json, status_code) -> KAgentSessionService."""
    def _factory(response_json: dict | None, status_code: int = 200) -> KAgentSessionService:
        return KAgentSessionService(get_client(response_json, status_code))
    return _factory


# ---------------------------------------------------------------------------
# get_session tests (restored from original — including regression guards)
# ---------------------------------------------------------------------------

class TestGetSession:

    @pytest.mark.asyncio
    async def test_returns_none_on_404(self, get_client):
        """A 404 response returns None without raising."""
        svc = KAgentSessionService(get_client(response_json=None, status_code=404))
        session = await svc.get_session(app_name="app", user_id="u1", session_id="missing")
        assert session is None

    @pytest.mark.asyncio
    async def test_returns_none_when_no_data(self, get_session_svc):
        """An empty data envelope returns None."""
        session = await get_session_svc({"data": None}).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is None

    @pytest.mark.asyncio
    async def test_event_ids_preserved(self, make_event, session_response, get_session_svc):
        """Event identity (id) is preserved after loading from the API."""
        events = [make_event("user"), make_event("assistant")]
        original_ids = [e.id for e in events]
        session = await get_session_svc(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is not None
        assert [e.id for e in session.events] == original_ids

    @pytest.mark.asyncio
    async def test_events_not_duplicated(self, make_event, session_response, get_session_svc):
        """Each event from the API must appear exactly once in session.events.

        Regression guard: Session(events=events) pre-populates session.events,
        and super().append_event() then appends again — causing duplication.
        """
        events = [make_event("user"), make_event("assistant"), make_event("tool")]
        session = await get_session_svc(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is not None
        assert len(session.events) == len(events), (
            f"Expected {len(events)} events but got {len(session.events)} — possible event duplication in get_session"
        )

    @pytest.mark.asyncio
    async def test_single_event_not_duplicated(self, make_event, session_response, get_session_svc):
        """Single-event case: still only one event in session.events."""
        events = [make_event("user")]
        session = await get_session_svc(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is not None
        assert len(session.events) == 1

    @pytest.mark.asyncio
    async def test_empty_events(self, session_response, get_session_svc):
        """Zero events from the API yields an empty session.events list."""
        session = await get_session_svc(session_response([])).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is not None
        assert len(session.events) == 0

    @pytest.mark.asyncio
    async def test_state_delta_applied_once(self, make_event, session_response, get_session_svc):
        """State deltas from events must be applied exactly once to session.state.

        Regression guard: double-appending events caused _update_session_state()
        to be called twice per event.
        """
        events = [make_event("assistant", state_delta={"counter": 7})]
        session = await get_session_svc(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is not None
        assert session.state.get("counter") == 7, (
            f"Expected state['counter'] == 7, got {session.state.get('counter')} — "
            "state_delta may have been applied more than once"
        )

    @pytest.mark.asyncio
    async def test_multiple_state_deltas_applied_once(self, make_event, session_response, get_session_svc):
        """Multiple events each contributing a state key are each applied once."""
        events = [
            make_event("assistant", state_delta={"key_a": "value_a"}),
            make_event("tool", state_delta={"key_b": "value_b"}),
        ]
        session = await get_session_svc(session_response(events)).get_session(app_name="app", user_id="u1", session_id="s1")
        assert session is not None
        assert session.state.get("key_a") == "value_a"
        assert session.state.get("key_b") == "value_b"


# ---------------------------------------------------------------------------
# append_event tests
# ---------------------------------------------------------------------------

class TestAppendEvent:
    """Tests for append_event method."""

    @pytest.mark.asyncio
    async def test_append_event_success(self, session_service, mock_client, sample_session, sample_event):
        """Test successful event append."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        result = await session_service.append_event(sample_session, sample_event)

        assert result == sample_event
        mock_client.post.assert_called_once()
        assert f"/api/sessions/{sample_session.id}/events" in mock_client.post.call_args[0][0]

    @pytest.mark.asyncio
    async def test_append_event_404_recovery(self, session_service, mock_client, sample_session, sample_event):
        """Test 404 triggers session recreation and retry."""
        mock_response_404 = mock.MagicMock()
        mock_response_404.status_code = 404

        mock_response_create = mock.MagicMock()
        mock_response_create.status_code = 201
        mock_response_create.raise_for_status = mock.MagicMock()

        mock_response_success = mock.MagicMock()
        mock_response_success.status_code = 201
        mock_response_success.raise_for_status = mock.MagicMock()

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}

        mock_client.post.side_effect = [mock_response_404, mock_response_create, mock_response_success]
        mock_client.get.return_value = mock_tasks_response

        result = await session_service.append_event(sample_session, sample_event)

        assert result == sample_event
        assert mock_client.post.call_count == 3
        assert mock_client.get.call_count == 1
        calls = mock_client.post.call_args_list
        assert f"/api/sessions/{sample_session.id}/events" in calls[0][0][0]
        assert "/api/sessions" == calls[1][0][0]
        assert f"/api/sessions/{sample_session.id}/events" in calls[2][0][0]

    @pytest.mark.asyncio
    async def test_append_event_404_retry_also_404(self, session_service, mock_client, sample_session, sample_event):
        """Test that a second 404 on retry raises without infinite recursion."""
        mock_response_404 = mock.MagicMock()
        mock_response_404.status_code = 404

        mock_response_create = mock.MagicMock()
        mock_response_create.status_code = 201
        mock_response_create.raise_for_status = mock.MagicMock()

        mock_response_retry_404 = mock.MagicMock()
        mock_response_retry_404.status_code = 404
        mock_response_retry_404.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Not found", request=mock.MagicMock(), response=mock_response_retry_404
        )

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}

        mock_client.post.side_effect = [mock_response_404, mock_response_create, mock_response_retry_404]
        mock_client.get.return_value = mock_tasks_response

        with pytest.raises(httpx.HTTPStatusError):
            await session_service.append_event(sample_session, sample_event)

        # Exactly 3 POST calls: initial, recreation, retry — no infinite loop
        assert mock_client.post.call_count == 3

    @pytest.mark.asyncio
    async def test_append_event_404_recreation_fails_raises_runtime_error(
        self, session_service, mock_client, sample_session, sample_event
    ):
        """Test 404 + recreation failure raises RuntimeError with 404 context."""
        mock_response_404 = mock.MagicMock()
        mock_response_404.status_code = 404

        mock_response_create_500 = mock.MagicMock()
        mock_response_create_500.status_code = 500
        mock_response_create_500.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error", request=mock.MagicMock(), response=mock_response_create_500
        )

        mock_client.post.side_effect = [mock_response_404, mock_response_create_500]

        with pytest.raises(RuntimeError, match="not found.*recreation failed"):
            await session_service.append_event(sample_session, sample_event)

    @pytest.mark.asyncio
    async def test_append_event_404_recovery_failure(self, session_service, mock_client, sample_session, sample_event):
        """Test 404 recovery where retry fails with a 500."""
        mock_response_404 = mock.MagicMock()
        mock_response_404.status_code = 404

        mock_response_create = mock.MagicMock()
        mock_response_create.status_code = 201
        mock_response_create.raise_for_status = mock.MagicMock()

        mock_response_retry_fail = mock.MagicMock()
        mock_response_retry_fail.status_code = 500
        mock_response_retry_fail.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error", request=mock.MagicMock(), response=mock_response_retry_fail
        )

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}

        mock_client.post.side_effect = [mock_response_404, mock_response_create, mock_response_retry_fail]
        mock_client.get.return_value = mock_tasks_response

        with pytest.raises(httpx.HTTPStatusError):
            await session_service.append_event(sample_session, sample_event)

        assert mock_client.post.call_count == 3

    @pytest.mark.asyncio
    async def test_append_event_non_404_error(self, session_service, mock_client, sample_session, sample_event):
        """Test non-404 errors are raised immediately without recovery."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 500
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error", request=mock.MagicMock(), response=mock_response
        )
        mock_client.post.return_value = mock_response

        with pytest.raises(httpx.HTTPStatusError):
            await session_service.append_event(sample_session, sample_event)

        mock_client.post.assert_called_once()


# ---------------------------------------------------------------------------
# _recreate_session tests
# ---------------------------------------------------------------------------

class TestRecreateSession:
    """Tests for _recreate_session method."""

    @pytest.mark.asyncio
    async def test_recreate_session_success(self, session_service, mock_client, sample_session):
        """Test successful session recreation."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(sample_session)

        mock_client.post.assert_called_once()
        request_data = mock_client.post.call_args[1]["json"]
        assert request_data["id"] == sample_session.id
        assert request_data["user_id"] == sample_session.user_id
        assert request_data["agent_ref"] == sample_session.app_name
        mock_client.get.assert_called_once()

    @pytest.mark.asyncio
    async def test_recreate_session_with_session_name(self, session_service, mock_client, sample_session):
        """Test session recreation includes session_name from state."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(sample_session)

        request_data = mock_client.post.call_args[1]["json"]
        assert request_data["name"] == "Test Session"

    @pytest.mark.asyncio
    async def test_recreate_session_preserves_source(self, session_service, mock_client):
        """Test session recreation preserves the source field from state."""
        session_with_source = Session(
            id="sess-src",
            user_id="u1",
            app_name="app",
            state={"session_name": "My Session", "source": "web"},
        )
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(session_with_source)

        request_data = mock_client.post.call_args[1]["json"]
        assert request_data["source"] == "web"
        assert request_data["name"] == "My Session"

    @pytest.mark.asyncio
    async def test_recreate_session_state_loss_warning_for_unknown_fields(
        self, session_service, mock_client, caplog
    ):
        """Warning fires for state fields beyond session_name and source."""
        session_with_extra = Session(
            id="sess-extra",
            user_id="u1",
            app_name="app",
            state={"session_name": "S", "source": "web", "custom_key": "custom_value"},
        )
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        import logging
        with caplog.at_level(logging.WARNING):
            await session_service._recreate_session(session_with_extra)

        assert any("custom_key" in record.message for record in caplog.records)

    @pytest.mark.asyncio
    async def test_recreate_session_no_warning_for_known_fields_only(
        self, session_service, mock_client, caplog
    ):
        """No warning fires when only known fields (session_name, source) are in state."""
        session_known_only = Session(
            id="sess-known",
            user_id="u1",
            app_name="app",
            state={"session_name": "S", "source": "web"},
        )
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        import logging
        with caplog.at_level(logging.WARNING):
            await session_service._recreate_session(session_known_only)

        assert not any("additional state fields" in record.message for record in caplog.records)

    @pytest.mark.asyncio
    async def test_recreate_session_failure(self, session_service, mock_client, sample_session):
        """Test session recreation failure raises error."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 500
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error", request=mock.MagicMock(), response=mock_response
        )
        mock_client.post.return_value = mock_response

        with pytest.raises(httpx.HTTPStatusError):
            await session_service._recreate_session(sample_session)

    @pytest.mark.asyncio
    async def test_recreate_session_with_inflight_task(self, session_service, mock_client, sample_session):
        """Test session recreation detects in-flight tasks."""
        mock_create_response = mock.MagicMock()
        mock_create_response.status_code = 201
        mock_create_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_create_response

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {
            "data": [{"id": "task-123", "status": {"state": "working", "message": "Processing..."}}]
        }
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(sample_session)

        assert mock_client.post.call_count == 1
        assert mock_client.get.call_count == 1
        assert f"/api/sessions/{sample_session.id}/tasks" in mock_client.get.call_args[0][0]

    @pytest.mark.asyncio
    async def test_recreate_session_409_treated_as_success(self, session_service, mock_client, sample_session):
        """409 Conflict during recreation is treated as success (concurrent recreation)."""
        mock_response_409 = mock.MagicMock()
        mock_response_409.status_code = 409
        mock_client.post.return_value = mock_response_409

        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(sample_session)

        mock_client.post.assert_called_once()
        mock_client.get.assert_called_once()
