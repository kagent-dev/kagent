"""Tests for kagent.adk._session_service module."""

import pytest
from unittest.mock import Mock, AsyncMock, MagicMock, patch
import httpx
from google.adk.events.event import Event
from google.adk.sessions import Session
from google.adk.sessions.base_session_service import GetSessionConfig

from kagent.adk._session_service import KAgentSessionService


class TestKAgentSessionServiceInit:
    """Tests for KAgentSessionService initialization."""

    def test_init(self):
        """Test service initialization."""
        mock_client = Mock(spec=httpx.AsyncClient)
        service = KAgentSessionService(client=mock_client)
        
        assert service.client == mock_client


class TestCreateSession:
    """Tests for create_session method."""

    @pytest.mark.asyncio
    async def test_create_session_basic(self):
        """Test basic session creation."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.json.return_value = {
            "data": {
                "id": "session-123",
                "user_id": "user-456"
            }
        }
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = await service.create_session(
            app_name="test-app",
            user_id="user-456"
        )
        
        assert session.id == "session-123"
        assert session.user_id == "user-456"
        assert session.app_name == "test-app"
        assert session.state == {}
        
        mock_client.post.assert_called_once_with(
            "/api/sessions",
            json={"user_id": "user-456", "agent_ref": "test-app"},
            headers={"X-User-ID": "user-456"}
        )

    @pytest.mark.asyncio
    async def test_create_session_with_state(self):
        """Test session creation with initial state."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.json.return_value = {
            "data": {
                "id": "session-123",
                "user_id": "user-456"
            }
        }
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        initial_state = {"key": "value", "count": 42}
        session = await service.create_session(
            app_name="test-app",
            user_id="user-456",
            state=initial_state
        )
        
        assert session.state == initial_state

    @pytest.mark.asyncio
    async def test_create_session_with_session_id(self):
        """Test session creation with specific session ID."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.json.return_value = {
            "data": {
                "id": "custom-session-id",
                "user_id": "user-456"
            }
        }
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = await service.create_session(
            app_name="test-app",
            user_id="user-456",
            session_id="custom-session-id"
        )
        
        mock_client.post.assert_called_once_with(
            "/api/sessions",
            json={
                "user_id": "user-456",
                "agent_ref": "test-app",
                "id": "custom-session-id"
            },
            headers={"X-User-ID": "user-456"}
        )

    @pytest.mark.asyncio
    async def test_create_session_http_error(self):
        """Test session creation with HTTP error."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Error", request=Mock(), response=Mock()
        )
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        with pytest.raises(httpx.HTTPStatusError):
            await service.create_session(
                app_name="test-app",
                user_id="user-456"
            )

    @pytest.mark.asyncio
    async def test_create_session_no_data(self):
        """Test session creation when response has no data."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.json.return_value = {"message": "Error occurred"}
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        with pytest.raises(RuntimeError, match="Failed to create session"):
            await service.create_session(
                app_name="test-app",
                user_id="user-456"
            )


class TestGetSession:
    """Tests for get_session method."""

    @pytest.mark.asyncio
    async def test_get_session_basic(self):
        """Test basic session retrieval."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": {
                "session": {
                    "id": "session-123",
                    "user_id": "user-456"
                },
                "events": []
            }
        }
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = await service.get_session(
            app_name="test-app",
            user_id="user-456",
            session_id="session-123"
        )
        
        assert session is not None
        assert session.id == "session-123"
        assert session.user_id == "user-456"
        assert len(session.events) == 0
        
        mock_client.get.assert_called_once()
        call_args = mock_client.get.call_args
        assert "/api/sessions/session-123" in call_args[0][0]
        assert "limit=-1" in call_args[0][0]

    @pytest.mark.asyncio
    async def test_get_session_not_found(self):
        """Test get_session when session doesn't exist."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.status_code = 404
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = await service.get_session(
            app_name="test-app",
            user_id="user-456",
            session_id="non-existent"
        )
        
        assert session is None

    @pytest.mark.asyncio
    async def test_get_session_with_config(self):
        """Test get_session with configuration."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": {
                "session": {
                    "id": "session-123",
                    "user_id": "user-456"
                },
                "events": []
            }
        }
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        config = GetSessionConfig(num_recent_events=10)
        session = await service.get_session(
            app_name="test-app",
            user_id="user-456",
            session_id="session-123",
            config=config
        )
        
        assert session is not None
        call_args = mock_client.get.call_args
        assert "limit=10" in call_args[0][0]

    @pytest.mark.asyncio
    async def test_get_session_with_events(self):
        """Test get_session with events in response."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        
        # Create a mock event
        event_json = '{"id":"event-1","timestamp":"2024-01-01T00:00:00Z"}'
        
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "data": {
                "session": {
                    "id": "session-123",
                    "user_id": "user-456"
                },
                "events": [
                    {"data": event_json}
                ]
            }
        }
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        with patch('google.adk.events.event.Event.model_validate_json') as mock_validate:
            mock_event = Mock(spec=Event)
            mock_validate.return_value = mock_event
            
            session = await service.get_session(
                app_name="test-app",
                user_id="user-456",
                session_id="session-123"
            )
            
            assert session is not None
            assert len(session.events) == 1
            assert session.events[0] == mock_event

    @pytest.mark.asyncio
    async def test_get_session_http_error(self):
        """Test get_session with HTTP error."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        
        mock_response = Mock()
        mock_response.status_code = 500
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Server error",
            request=Mock(),
            response=mock_response
        )
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        with pytest.raises(httpx.HTTPStatusError):
            await service.get_session(
                app_name="test-app",
                user_id="user-456",
                session_id="session-123"
            )

    @pytest.mark.asyncio
    async def test_get_session_no_data(self):
        """Test get_session when response has no data."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {}
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = await service.get_session(
            app_name="test-app",
            user_id="user-456",
            session_id="session-123"
        )
        
        assert session is None

    @pytest.mark.asyncio
    async def test_get_session_no_session_in_data(self):
        """Test get_session when data has no session field."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"data": {}}
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = await service.get_session(
            app_name="test-app",
            user_id="user-456",
            session_id="session-123"
        )
        
        assert session is None


class TestListSessions:
    """Tests for list_sessions method."""

    @pytest.mark.asyncio
    async def test_list_sessions_empty(self):
        """Test listing sessions with no sessions."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.json.return_value = {"data": []}
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        result = await service.list_sessions(
            app_name="test-app",
            user_id="user-456"
        )
        
        assert len(result.sessions) == 0

    @pytest.mark.asyncio
    async def test_list_sessions_multiple(self):
        """Test listing multiple sessions."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.json.return_value = {
            "data": [
                {"id": "session-1", "user_id": "user-456"},
                {"id": "session-2", "user_id": "user-456"},
                {"id": "session-3", "user_id": "user-456"}
            ]
        }
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        result = await service.list_sessions(
            app_name="test-app",
            user_id="user-456"
        )
        
        assert len(result.sessions) == 3
        assert result.sessions[0].id == "session-1"
        assert result.sessions[1].id == "session-2"
        assert result.sessions[2].id == "session-3"
        
        mock_client.get.assert_called_once_with(
            "/api/sessions?user_id=user-456",
            headers={"X-User-ID": "user-456"}
        )

    @pytest.mark.asyncio
    async def test_list_sessions_http_error(self):
        """Test list_sessions with HTTP error."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Error", request=Mock(), response=Mock()
        )
        mock_client.get.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        with pytest.raises(httpx.HTTPStatusError):
            await service.list_sessions(
                app_name="test-app",
                user_id="user-456"
            )

    def test_list_sessions_sync_not_implemented(self):
        """Test that sync version raises NotImplementedError."""
        mock_client = Mock(spec=httpx.AsyncClient)
        service = KAgentSessionService(client=mock_client)
        
        with pytest.raises(NotImplementedError, match="not supported"):
            service.list_sessions_sync(
                app_name="test-app",
                user_id="user-456"
            )


class TestDeleteSession:
    """Tests for delete_session method."""

    @pytest.mark.asyncio
    async def test_delete_session_success(self):
        """Test successful session deletion."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_client.delete.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        await service.delete_session(
            app_name="test-app",
            user_id="user-456",
            session_id="session-123"
        )
        
        mock_client.delete.assert_called_once_with(
            "/api/sessions/session-123?user_id=user-456",
            headers={"X-User-ID": "user-456"}
        )

    @pytest.mark.asyncio
    async def test_delete_session_http_error(self):
        """Test delete_session with HTTP error."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Error", request=Mock(), response=Mock()
        )
        mock_client.delete.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        with pytest.raises(httpx.HTTPStatusError):
            await service.delete_session(
                app_name="test-app",
                user_id="user-456",
                session_id="session-123"
            )


class TestAppendEvent:
    """Tests for append_event method."""

    @pytest.mark.asyncio
    async def test_append_event_success(self):
        """Test successful event appending."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        # Create mock session and event
        session = Session(
            id="session-123",
            user_id="user-456",
            app_name="test-app",
            state={}
        )
        
        event = Mock(spec=Event)
        event.id = "event-1"
        event.timestamp = "2024-01-01T00:00:00Z"
        event.partial = False  # Add attributes checked by base class
        event.actions = []  # Required by base class
        event.model_dump_json.return_value = '{"id":"event-1"}'
        
        result = await service.append_event(session, event)
        
        assert result == event
        assert session.last_update_time == event.timestamp
        
        mock_client.post.assert_called_once_with(
            "/api/sessions/session-123/events?user_id=user-456",
            json={"id": "event-1", "data": '{"id":"event-1"}'},
            headers={"X-User-ID": "user-456"}
        )

    @pytest.mark.asyncio
    async def test_append_event_http_error(self):
        """Test append_event with HTTP error."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = httpx.HTTPStatusError(
            "Error", request=Mock(), response=Mock()
        )
        mock_client.post.return_value = mock_response
        
        service = KAgentSessionService(client=mock_client)
        
        session = Session(
            id="session-123",
            user_id="user-456",
            app_name="test-app",
            state={}
        )
        
        event = Mock(spec=Event)
        event.id = "event-1"
        event.model_dump_json.return_value = '{"id":"event-1"}'
        
        with pytest.raises(httpx.HTTPStatusError):
            await service.append_event(session, event)


class TestIntegration:
    """Integration tests for session service."""

    @pytest.mark.asyncio
    async def test_create_and_retrieve_session(self):
        """Integration test: create and retrieve a session."""
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        
        # Mock create response
        create_response = Mock()
        create_response.json.return_value = {
            "data": {
                "id": "session-new",
                "user_id": "user-test"
            }
        }
        
        # Mock get response
        get_response = Mock()
        get_response.status_code = 200
        get_response.json.return_value = {
            "data": {
                "session": {
                    "id": "session-new",
                    "user_id": "user-test"
                },
                "events": []
            }
        }
        
        mock_client.post.return_value = create_response
        mock_client.get.return_value = get_response
        
        service = KAgentSessionService(client=mock_client)
        
        # Create session
        created = await service.create_session(
            app_name="integration-test",
            user_id="user-test"
        )
        
        # Retrieve session
        retrieved = await service.get_session(
            app_name="integration-test",
            user_id="user-test",
            session_id=created.id
        )
        
        assert created.id == retrieved.id
        assert created.user_id == retrieved.user_id

