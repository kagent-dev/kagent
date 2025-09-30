import asyncio
from unittest.mock import AsyncMock, MagicMock, mock_open, patch

import pytest

from kagent.adk._service_account_service import KAgentServiceAccountService


class TestKAgentServiceAccountService:
    """Test cases for KAgentServiceAccountService."""

    @pytest.fixture
    def service_account_service(self):
        """Create service account service for testing."""
        return KAgentServiceAccountService(app_name="test-app")

    @pytest.mark.asyncio
    async def test_read_service_account_info_success(self, service_account_service):
        """Test successful reading of service account information."""
        # Mock file contents
        mock_token = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.jwt"

        with patch("kagent.adk._service_account_service.asyncio.to_thread") as mock_to_thread:
            mock_to_thread.return_value = mock_token

            await service_account_service._read_service_account_info()

            assert service_account_service.service_account_token == mock_token

    @pytest.mark.asyncio
    async def test_read_service_account_info_failure(self, service_account_service):
        """Test handling of file read failures."""
        with patch("kagent.adk._service_account_service.asyncio.to_thread") as mock_to_thread:
            mock_to_thread.side_effect = Exception("File not found")

            await service_account_service._read_service_account_info()

            # Should handle errors gracefully
            assert service_account_service.service_account_token is None

    @pytest.mark.asyncio
    async def test_get_actor_jwt_success(self, service_account_service):
        """Test successful retrieval of actor JWT."""
        service_account_service.service_account_token = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.jwt"

        result = await service_account_service.get_actor_jwt()

        assert result == "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.jwt"

    @pytest.mark.asyncio
    async def test_get_actor_jwt_no_token(self, service_account_service):
        """Test handling when no service account token is available."""
        service_account_service.service_account_token = None

        result = await service_account_service.get_actor_jwt()

        assert result is None

    @pytest.mark.asyncio
    async def test_lifespan_context_manager(self, service_account_service):
        """Test the lifespan context manager."""
        with patch.object(service_account_service, "_update_token_loop") as mock_update:
            with patch.object(service_account_service, "_drain") as mock_drain:
                lifespan = service_account_service.lifespan()

                async with lifespan(MagicMock()):
                    pass

                mock_update.assert_called_once()
                mock_drain.assert_called_once()

    def test_read_file_sync(self, service_account_service):
        """Test synchronous file reading."""
        test_content = "test-file-content"

        with patch("builtins.open", mock_open(read_data=test_content)):
            result = service_account_service._read_file_sync("/test/path")

            assert result == test_content

    @pytest.mark.asyncio
    async def test_drain_cancels_task(self, service_account_service):
        """Test that drain properly cancels the update task."""
        # Create a mock task
        mock_task = AsyncMock()
        service_account_service.update_task = mock_task

        service_account_service._drain()

        mock_task.cancel.assert_called_once()

    @pytest.mark.asyncio
    async def test_drain_no_task(self, service_account_service):
        """Test drain when no task is running."""
        # No task set
        service_account_service.update_task = None

        # Should not raise an error
        service_account_service._drain()
