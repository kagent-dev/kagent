"""Tests for kagent.adk._token module."""

import asyncio
import pytest
from pathlib import Path
from unittest.mock import Mock, patch, mock_open, AsyncMock, MagicMock
import httpx

from kagent.adk._token import KAgentTokenService, read_token, KAGENT_TOKEN_PATH


class TestReadToken:
    """Tests for read_token function."""

    def test_read_token_success(self):
        """Test successful token reading."""
        mock_file_content = "test-token-12345\n"
        with patch("builtins.open", mock_open(read_data=mock_file_content)):
            token = read_token()
            assert token == "test-token-12345"

    def test_read_token_with_whitespace(self):
        """Test token reading strips whitespace."""
        mock_file_content = "  \n  test-token  \n  "
        with patch("builtins.open", mock_open(read_data=mock_file_content)):
            token = read_token()
            assert token == "test-token"

    def test_read_token_file_not_found(self):
        """Test token reading when file doesn't exist."""
        with patch("builtins.open", side_effect=FileNotFoundError("File not found")):
            token = read_token()
            assert token is None

    def test_read_token_permission_denied(self):
        """Test token reading when permission is denied."""
        with patch("builtins.open", side_effect=PermissionError("Permission denied")):
            token = read_token()
            assert token is None

    def test_read_token_os_error(self):
        """Test token reading with generic OS error."""
        with patch("builtins.open", side_effect=OSError("Generic OS error")):
            token = read_token()
            assert token is None

    def test_read_token_uses_correct_path(self):
        """Test that read_token uses the correct token path."""
        with patch("builtins.open", mock_open(read_data="token")) as mock_file:
            read_token()
            mock_file.assert_called_once_with(KAGENT_TOKEN_PATH, "r", encoding="utf-8")


class TestKAgentTokenService:
    """Tests for KAgentTokenService class."""

    def test_init(self):
        """Test KAgentTokenService initialization."""
        service = KAgentTokenService(app_name="test-app")
        
        assert service.app_name == "test-app"
        assert service.token is None
        assert service.update_task is None
        assert service.update_lock is not None

    def test_event_hooks_returns_dict(self):
        """Test that event_hooks returns a proper dict."""
        service = KAgentTokenService(app_name="test-app")
        hooks = service.event_hooks()
        
        assert isinstance(hooks, dict)
        assert "request" in hooks
        assert isinstance(hooks["request"], list)
        assert len(hooks["request"]) == 1

    @pytest.mark.asyncio
    async def test_get_token(self):
        """Test _get_token method."""
        service = KAgentTokenService(app_name="test-app")
        service.token = "test-token"
        
        token = await service._get_token()
        assert token == "test-token"

    @pytest.mark.asyncio
    async def test_get_token_none(self):
        """Test _get_token when token is None."""
        service = KAgentTokenService(app_name="test-app")
        
        token = await service._get_token()
        assert token is None

    @pytest.mark.asyncio
    async def test_read_kagent_token(self):
        """Test _read_kagent_token method."""
        service = KAgentTokenService(app_name="test-app")
        
        with patch("kagent.adk._token.read_token", return_value="mock-token"):
            token = await service._read_kagent_token()
            assert token == "mock-token"

    @pytest.mark.asyncio
    async def test_read_kagent_token_none(self):
        """Test _read_kagent_token when token file doesn't exist."""
        service = KAgentTokenService(app_name="test-app")
        
        with patch("kagent.adk._token.read_token", return_value=None):
            token = await service._read_kagent_token()
            assert token is None

    @pytest.mark.asyncio
    async def test_add_bearer_token_with_token(self):
        """Test _add_bearer_token adds correct headers when token exists."""
        service = KAgentTokenService(app_name="test-app")
        service.token = "my-token"
        
        request = httpx.Request("GET", "https://example.com")
        await service._add_bearer_token(request)
        
        assert request.headers["Authorization"] == "Bearer my-token"
        assert request.headers["X-Agent-Name"] == "test-app"

    @pytest.mark.asyncio
    async def test_add_bearer_token_without_token(self):
        """Test _add_bearer_token without token."""
        service = KAgentTokenService(app_name="test-app")
        service.token = None
        
        request = httpx.Request("GET", "https://example.com")
        await service._add_bearer_token(request)
        
        assert "Authorization" not in request.headers
        assert request.headers["X-Agent-Name"] == "test-app"

    @pytest.mark.asyncio
    async def test_add_bearer_token_empty_token(self):
        """Test _add_bearer_token with empty token."""
        service = KAgentTokenService(app_name="test-app")
        service.token = ""
        
        request = httpx.Request("GET", "https://example.com")
        await service._add_bearer_token(request)
        
        # Empty string is falsy, so no Authorization header
        assert "Authorization" not in request.headers
        assert request.headers["X-Agent-Name"] == "test-app"

    @pytest.mark.asyncio
    async def test_update_token_loop(self):
        """Test _update_token_loop initializes token and creates task."""
        service = KAgentTokenService(app_name="test-app")
        
        with patch.object(service, "_read_kagent_token", return_value="initial-token"):
            with patch.object(service, "_refresh_token", return_value=asyncio.Future()) as mock_refresh:
                mock_refresh.return_value.set_result(None)
                
                await service._update_token_loop()
                
                assert service.token == "initial-token"
                assert service.update_task is not None

    @pytest.mark.asyncio
    async def test_refresh_token_updates_when_changed(self):
        """Test _refresh_token updates token when it changes."""
        service = KAgentTokenService(app_name="test-app")
        service.token = "old-token"
        
        call_count = 0
        
        async def mock_read():
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                return "new-token"
            # Cancel after first iteration to prevent infinite loop
            raise asyncio.CancelledError()
        
        async def mock_sleep(duration):
            # Allow first sleep to pass, then cancel
            pass
        
        with patch.object(service, "_read_kagent_token", side_effect=mock_read):
            with patch("asyncio.sleep", side_effect=mock_sleep):
                try:
                    # Create a proper asyncio task
                    service.update_task = asyncio.create_task(service._refresh_token())
                    await service.update_task
                except asyncio.CancelledError:
                    pass
                
                assert service.token == "new-token"

    @pytest.mark.asyncio
    async def test_refresh_token_skips_none(self):
        """Test _refresh_token doesn't update when new token is None."""
        service = KAgentTokenService(app_name="test-app")
        service.token = "existing-token"
        
        call_count = 0
        
        async def mock_read():
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                return None
            raise asyncio.CancelledError()
        
        with patch.object(service, "_read_kagent_token", side_effect=mock_read):
            with patch("asyncio.sleep", return_value=None):
                try:
                    service.update_task = asyncio.create_task(service._refresh_token())
                    await service.update_task
                except asyncio.CancelledError:
                    pass
                
                # Token should not change when new token is None
                assert service.token == "existing-token"

    @pytest.mark.asyncio
    async def test_refresh_token_skips_same_token(self):
        """Test _refresh_token doesn't update when token hasn't changed."""
        service = KAgentTokenService(app_name="test-app")
        service.token = "same-token"
        
        call_count = 0
        
        async def mock_read():
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                return "same-token"
            raise asyncio.CancelledError()
        
        with patch.object(service, "_read_kagent_token", side_effect=mock_read):
            with patch("asyncio.sleep", return_value=None):
                try:
                    service.update_task = asyncio.create_task(service._refresh_token())
                    await service.update_task
                except asyncio.CancelledError:
                    pass
                
                # Token should remain the same
                assert service.token == "same-token"

    def test_drain_cancels_task(self):
        """Test _drain cancels the update task."""
        service = KAgentTokenService(app_name="test-app")
        service.update_task = Mock()
        
        service._drain()
        
        service.update_task.cancel.assert_called_once()

    def test_drain_without_task(self):
        """Test _drain when no task exists."""
        service = KAgentTokenService(app_name="test-app")
        service.update_task = None
        
        # Should not raise an exception
        service._drain()

    @pytest.mark.asyncio
    async def test_lifespan_context_manager(self):
        """Test lifespan returns a working async context manager."""
        service = KAgentTokenService(app_name="test-app")
        
        with patch.object(service, "_update_token_loop", return_value=None):
            with patch.object(service, "_drain", return_value=None):
                lifespan = service.lifespan()
                
                mock_app = Mock()
                async with lifespan(mock_app):
                    # Inside the context
                    pass
                
                # Verify lifecycle methods were called
                service._update_token_loop.assert_called_once()
                service._drain.assert_called_once()

    @pytest.mark.asyncio
    async def test_lifespan_exception_handling(self):
        """Test lifespan handles exceptions properly."""
        service = KAgentTokenService(app_name="test-app")
        
        with patch.object(service, "_update_token_loop", new_callable=AsyncMock):
            lifespan = service.lifespan()
            mock_app = Mock()
            
            # Verify exception is propagated
            with pytest.raises(ValueError):
                async with lifespan(mock_app):
                    raise ValueError("Test error")

    @pytest.mark.asyncio
    async def test_integration_token_lifecycle(self):
        """Integration test for complete token lifecycle."""
        service = KAgentTokenService(app_name="integration-test")
        
        with patch("kagent.adk._token.read_token", return_value="integration-token"):
            # Initialize
            await service._update_token_loop()
            
            assert service.token == "integration-token"
            assert service.update_task is not None
            
            # Test token in request
            request = httpx.Request("GET", "https://example.com")
            await service._add_bearer_token(request)
            
            assert request.headers["Authorization"] == "Bearer integration-token"
            assert request.headers["X-Agent-Name"] == "integration-test"
            
            # Cleanup
            service._drain()
            
            # Wait a bit for cancellation to take effect
            await asyncio.sleep(0.01)
            assert service.update_task.cancelled() or service.update_task.done()


class TestTokenServiceWithLock:
    """Tests for token service with concurrent access."""

    @pytest.mark.asyncio
    async def test_concurrent_token_access(self):
        """Test that concurrent token access is properly locked."""
        service = KAgentTokenService(app_name="test-app")
        service.token = "initial-token"
        
        results = []
        
        async def reader():
            token = await service._get_token()
            results.append(token)
        
        # Launch multiple concurrent readers
        await asyncio.gather(*[reader() for _ in range(10)])
        
        # All should have gotten the same token
        assert all(t == "initial-token" for t in results)
        assert len(results) == 10

