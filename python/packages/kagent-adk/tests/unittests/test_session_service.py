from unittest import mock

import httpx
import pytest
from google.adk.events.event import Event
from google.adk.sessions import Session

from kagent.adk._session_service import KAgentSessionService


@pytest.fixture
def mock_client():
    """Create a mock httpx.AsyncClient."""
    return mock.AsyncMock(spec=httpx.AsyncClient)


@pytest.fixture
def session_service(mock_client):
    """Create a KAgentSessionService with mocked client."""
    return KAgentSessionService(client=mock_client)


@pytest.fixture
def sample_session():
    """Create a sample session for testing."""
    return Session(
        id="test-session-123",
        user_id="test-user",
        app_name="test-app",
        state={"session_name": "Test Session"},
    )


@pytest.fixture
def sample_event():
    """Create a sample event for testing."""
    return Event(
        invocation_id="test-invocation",
        author="user",
    )


class TestAppendEvent:
    """Tests for append_event method."""

    @pytest.mark.asyncio
    async def test_append_event_success(self, session_service, mock_client, sample_session, sample_event):
        """Test successful event append."""
        # Mock successful response
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        result = await session_service.append_event(sample_session, sample_event)

        assert result == sample_event
        mock_client.post.assert_called_once()
        call_args = mock_client.post.call_args
        assert f"/api/sessions/{sample_session.id}/events" in call_args[0][0]

    @pytest.mark.asyncio
    async def test_append_event_404_recovery(self, session_service, mock_client, sample_session, sample_event):
        """Test 404 triggers session recreation and retry."""
        # First call returns 404, second call (after recreation) succeeds
        mock_response_404 = mock.MagicMock()
        mock_response_404.status_code = 404

        mock_response_success = mock.MagicMock()
        mock_response_success.status_code = 201
        mock_response_success.raise_for_status = mock.MagicMock()

        mock_response_create = mock.MagicMock()
        mock_response_create.status_code = 201
        mock_response_create.raise_for_status = mock.MagicMock()

        # Mock tasks fetch response (no tasks found)
        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}

        # Configure mock to return different responses
        mock_client.post.side_effect = [
            mock_response_404,  # First append attempt (404)
            mock_response_create,  # Session recreation
            mock_response_success,  # Retry append (success)
        ]
        mock_client.get.return_value = mock_tasks_response  # Tasks fetch

        result = await session_service.append_event(sample_session, sample_event)

        assert result == sample_event
        # Should be called 3 times: initial append, session create, retry append
        assert mock_client.post.call_count == 3
        # Should fetch tasks once after recreation
        assert mock_client.get.call_count == 1

        # Verify the calls
        calls = mock_client.post.call_args_list
        assert f"/api/sessions/{sample_session.id}/events" in calls[0][0][0]  # First append
        assert "/api/sessions" == calls[1][0][0]  # Session recreation
        assert f"/api/sessions/{sample_session.id}/events" in calls[2][0][0]  # Retry append

        # Verify tasks fetch
        get_call = mock_client.get.call_args
        assert f"/api/sessions/{sample_session.id}/tasks" in get_call[0][0]

    @pytest.mark.asyncio
    async def test_append_event_404_recovery_failure(self, session_service, mock_client, sample_session, sample_event):
        """Test 404 recovery fails if retry also fails."""
        # First call returns 404, recreation succeeds, but retry fails
        mock_response_404 = mock.MagicMock()
        mock_response_404.status_code = 404

        mock_response_create = mock.MagicMock()
        mock_response_create.status_code = 201
        mock_response_create.raise_for_status = mock.MagicMock()

        mock_response_retry_fail = mock.MagicMock()
        mock_response_retry_fail.status_code = 500
        mock_response_retry_fail.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error",
            request=mock.MagicMock(),
            response=mock_response_retry_fail,
        )

        # Mock tasks fetch
        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}

        mock_client.post.side_effect = [
            mock_response_404,  # First append (404)
            mock_response_create,  # Session recreation
            mock_response_retry_fail,  # Retry append (500)
        ]
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
            "Server error",
            request=mock.MagicMock(),
            response=mock_response,
        )
        mock_client.post.return_value = mock_response

        with pytest.raises(httpx.HTTPStatusError):
            await session_service.append_event(sample_session, sample_event)

        # Should only be called once (no retry for non-404 errors)
        mock_client.post.assert_called_once()


class TestRecreateSession:
    """Tests for _recreate_session method."""

    @pytest.mark.asyncio
    async def test_recreate_session_success(self, session_service, mock_client, sample_session):
        """Test successful session recreation."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        # Mock tasks fetch
        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        # Should not raise
        await session_service._recreate_session(sample_session)

        mock_client.post.assert_called_once()
        call_args = mock_client.post.call_args
        assert call_args[0][0] == "/api/sessions"

        # Verify request data
        request_data = call_args[1]["json"]
        assert request_data["id"] == sample_session.id
        assert request_data["user_id"] == sample_session.user_id
        assert request_data["agent_ref"] == sample_session.app_name

        # Verify tasks were fetched
        mock_client.get.assert_called_once()

    @pytest.mark.asyncio
    async def test_recreate_session_with_session_name(self, session_service, mock_client, sample_session):
        """Test session recreation includes session name from state."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 201
        mock_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_response

        # Mock tasks fetch
        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(sample_session)

        call_args = mock_client.post.call_args
        request_data = call_args[1]["json"]
        assert request_data["name"] == "Test Session"

    @pytest.mark.asyncio
    async def test_recreate_session_failure(self, session_service, mock_client, sample_session):
        """Test session recreation failure raises error."""
        mock_response = mock.MagicMock()
        mock_response.status_code = 500
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error",
            request=mock.MagicMock(),
            response=mock_response,
        )
        mock_client.post.return_value = mock_response

        with pytest.raises(httpx.HTTPStatusError):
            await session_service._recreate_session(sample_session)

    @pytest.mark.asyncio
    async def test_recreate_session_with_inflight_task(self, session_service, mock_client, sample_session):
        """Test session recreation detects in-flight tasks."""
        # Mock successful session creation
        mock_create_response = mock.MagicMock()
        mock_create_response.status_code = 201
        mock_create_response.raise_for_status = mock.MagicMock()
        mock_client.post.return_value = mock_create_response

        # Mock tasks response with an in-flight task
        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {
            "data": [{"id": "task-123", "status": {"state": "working", "message": "Processing..."}}]
        }
        mock_client.get.return_value = mock_tasks_response

        await session_service._recreate_session(sample_session)

        # Verify session creation call
        assert mock_client.post.call_count == 1
        # Verify tasks fetch call
        assert mock_client.get.call_count == 1
        get_call_url = mock_client.get.call_args[0][0]
        assert f"/api/sessions/{sample_session.id}/tasks" in get_call_url

    @pytest.mark.asyncio
    async def test_recreate_session_409_treated_as_success(self, session_service, mock_client, sample_session):
        """Test that 409 Conflict during recreation is treated as success (concurrent recreation)."""
        mock_response_409 = mock.MagicMock()
        mock_response_409.status_code = 409

        mock_client.post.return_value = mock_response_409

        # Mock tasks fetch (empty)
        mock_tasks_response = mock.MagicMock()
        mock_tasks_response.status_code = 200
        mock_tasks_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_tasks_response

        # Should not raise even though POST returned 409
        await session_service._recreate_session(sample_session)

        mock_client.post.assert_called_once()
        # Tasks fetch should still proceed after 409
        mock_client.get.assert_called_once()
