"""Tests for ADK integration classes (STS + token propagation)."""

from unittest.mock import AsyncMock, Mock, patch

import pytest
from google.adk.agents import LlmAgent
from google.adk.tools.mcp_tool.mcp_toolset import MCPToolset

from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from agentsts.adk._base import HEADERS_KEY
from agentsts.adk._base import _extract_jwt_expiry as extract_jwt_expiry
from agentsts.adk._base import _extract_jwt_from_headers as extract_jwt_from_headers
from agentsts.adk._base import _has_token_expired as has_token_expired
from agentsts.core import TokenType
from agentsts.core.client import TokenExchangeResponse


class TestADKTokenPropagationPlugin:
    """Unit tests for token propagation plugin covering: none, downstream, and STS exchange."""

    def _make_invocation_context(self, session_id: str, headers: dict | None):
        session = Mock()
        session.id = session_id
        session.state = {}
        if headers is not None:
            session.state[HEADERS_KEY] = headers
        invocation_context = Mock()
        invocation_context.session = session
        return invocation_context

    def _make_readonly_context(self, invocation_context):
        readonly_context = Mock()
        readonly_context._invocation_context = invocation_context
        return readonly_context

    def test_init(self):
        mock_sts_integration = Mock()
        plugin = ADKTokenPropagationPlugin(mock_sts_integration)
        assert plugin.name == "ADKTokenPropagationPlugin"
        assert plugin.sts_integration is mock_sts_integration
        assert plugin.token_cache == {}

    @pytest.mark.asyncio
    async def test_before_run_callback_no_headers(self):
        """Case: nothing added (no headers) -> no cache entry, returns None."""
        plugin = ADKTokenPropagationPlugin()
        ic = self._make_invocation_context("sess-1", headers=None)
        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None
            mock_logger.debug.assert_called_once_with("No subject token found in headers for token propagation")
        assert plugin.token_cache == {}

    @pytest.mark.asyncio
    async def test_downstream_token_propagation_without_sts(self):
        """Case: headers present, no STS integration -> subject token cached and available via header_provider."""
        plugin = ADKTokenPropagationPlugin(sts_integration=None)
        ic = self._make_invocation_context("sess-2", headers={"Authorization": "Bearer subj-token-123"})
        result = await plugin.before_run_callback(invocation_context=ic)
        assert result is None
        assert "sess-2" in plugin.token_cache
        assert plugin.token_cache["sess-2"].token == "subj-token-123"

        # propagate toolset
        mcp_toolset = Mock(spec=MCPToolset)
        agent = Mock(spec=LlmAgent)
        agent.tools = [mcp_toolset]
        plugin.add_to_agent(agent)
        # The toolset._header_provider should be callable
        assert callable(mcp_toolset._header_provider)

        # header provider should return subject token
        ro_ctx = self._make_readonly_context(ic)
        headers = plugin.header_provider(ro_ctx)
        assert headers == {"Authorization": "Bearer subj-token-123"}

        # cleanup - token should still be cached if not expired
        await plugin.after_run_callback(invocation_context=ic)
        # Token has no expiry, so it's preserved
        assert "sess-2" in plugin.token_cache

    @pytest.mark.asyncio
    async def test_sts_token_exchange_success(self):
        """Case: STS integration exchanges token -> access token cached and returned by header provider."""
        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = None
        sts._actor_token = "actor-token"
        sts.exchange_token = AsyncMock(return_value="access-token-XYZ")
        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-3", headers={"Authorization": "Bearer original-subject"})
        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None
            sts.exchange_token.assert_called_once_with(
                subject_token="original-subject",
                subject_token_type=TokenType.JWT,
                actor_token="actor-token",
                actor_token_type=TokenType.JWT,
            )
            # optional debug log length check
            mock_logger.debug.assert_called()  # at least one debug log
        assert "sess-3" in plugin.token_cache
        assert plugin.token_cache["sess-3"].token == "access-token-XYZ"

        ro_ctx = self._make_readonly_context(ic)
        headers = plugin.header_provider(ro_ctx)
        assert headers == {"Authorization": "Bearer access-token-XYZ"}

        # cleanup - token should still be cached if not expired
        await plugin.after_run_callback(invocation_context=ic)
        # Token has no expiry, so it's preserved
        assert "sess-3" in plugin.token_cache

    @pytest.mark.asyncio
    async def test_sts_token_exchange_failure(self):
        """Case: STS exchange raises -> no cache entry, graceful warning."""
        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = None
        sts._actor_token = "actor-token"
        sts.exchange_token = AsyncMock(side_effect=Exception("boom"))
        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-4", headers={"Authorization": "Bearer original-subject"})
        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None
            mock_logger.warning.assert_called_once()
        assert "sess-4" not in plugin.token_cache
        # header provider should yield empty dict
        ro_ctx = self._make_readonly_context(ic)
        assert plugin.header_provider(ro_ctx) == {}

    def test_header_provider_no_entry(self):
        """Case: header_provider called with no cached token -> returns empty dict."""
        plugin = ADKTokenPropagationPlugin()
        ic = self._make_invocation_context("sess-5", headers=None)
        ro_ctx = self._make_readonly_context(ic)
        # token_cache intentionally missing key should result in {}
        assert plugin.header_provider(ro_ctx) == {}

    @pytest.mark.asyncio
    async def test_after_run_callback_removes_expired_token(self):
        """Case: after_run_callback removes expired cached token."""
        import time

        past_expiry = int(time.time()) - 100

        plugin = ADKTokenPropagationPlugin()
        ic = self._make_invocation_context("sess-6", headers={"Authorization": "Bearer AAA"})

        # Mock expiry to return expired timestamp
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=past_expiry):
            await plugin.before_run_callback(invocation_context=ic)
            assert "sess-6" in plugin.token_cache

        # Token is expired, should be removed
        await plugin.after_run_callback(invocation_context=ic)
        assert "sess-6" not in plugin.token_cache

    @pytest.mark.asyncio
    async def test_dynamic_token_fetch_success_sync(self):
        """Case: sync fetch_actor_token is called successfully and token is exchanged."""
        fetch_token_mock = Mock(return_value="dynamic-actor-token")
        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = fetch_token_mock
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token-dynamic")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-7", headers={"Authorization": "Bearer subject-token"})

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None

            # Verify fetch_actor_token was called
            fetch_token_mock.assert_called_once()

            # Verify exchange_token was called with the dynamic token
            sts.exchange_token.assert_called_once_with(
                subject_token="subject-token",
                subject_token_type=TokenType.JWT,
                actor_token="dynamic-actor-token",
                actor_token_type=TokenType.JWT,
            )

            # Verify debug log for dynamic fetch
            debug_calls = [call.args[0] for call in mock_logger.debug.call_args_list]
            assert any("Fetched and cached new actor token" in call for call in debug_calls)

        # Verify token is cached
        assert "sess-7" in plugin.token_cache
        cache_entry = plugin.token_cache["sess-7"]
        assert cache_entry.token == "access-token-dynamic"

    @pytest.mark.asyncio
    async def test_dynamic_token_fetch_success_async(self):
        """Case: async fetch_actor_token is called successfully and token is exchanged."""

        async def async_fetch_token():
            return "dynamic-actor-token-async"

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = async_fetch_token
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token-dynamic-async")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-7a", headers={"Authorization": "Bearer subject-token"})

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None

            # Verify exchange_token was called with the dynamic token
            sts.exchange_token.assert_called_once_with(
                subject_token="subject-token",
                subject_token_type=TokenType.JWT,
                actor_token="dynamic-actor-token-async",
                actor_token_type=TokenType.JWT,
            )

            # Verify debug log for dynamic fetch
            debug_calls = [call.args[0] for call in mock_logger.debug.call_args_list]
            assert any("Fetched and cached new actor token" in call for call in debug_calls)

        # Verify token is cached
        assert "sess-7a" in plugin.token_cache
        cache_entry = plugin.token_cache["sess-7a"]
        assert cache_entry.token == "access-token-dynamic-async"

    @pytest.mark.asyncio
    async def test_dynamic_token_fetch_failure_sync(self):
        """Case: sync fetch_actor_token raises exception -> no token exchange, graceful handling."""
        fetch_token_mock = Mock(side_effect=Exception("Token fetch failed"))
        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = fetch_token_mock
        sts._actor_token = None

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-8", headers={"Authorization": "Bearer subject-token"})

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None

            # Verify fetch_actor_token was called
            fetch_token_mock.assert_called_once()

            # Verify warning was logged
            mock_logger.warning.assert_called_once()
            warning_msg = mock_logger.warning.call_args[0][0]
            assert "Failed to fetch actor token dynamically" in warning_msg

        # No token should be cached
        assert "sess-8" not in plugin.token_cache

    @pytest.mark.asyncio
    async def test_dynamic_token_fetch_failure_async(self):
        """Case: async fetch_actor_token raises exception -> no token exchange, graceful handling."""

        async def async_fetch_token_failing():
            raise Exception("Async token fetch failed")

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = async_fetch_token_failing
        sts._actor_token = None

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-8a", headers={"Authorization": "Bearer subject-token"})

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_run_callback(invocation_context=ic)
            assert result is None

            # Verify warning was logged
            mock_logger.warning.assert_called_once()
            warning_msg = mock_logger.warning.call_args[0][0]
            assert "Failed to fetch actor token dynamically" in warning_msg

        # No token should be cached
        assert "sess-8a" not in plugin.token_cache

    @pytest.mark.asyncio
    async def test_dynamic_token_preserved_when_not_expired(self):
        """Case: dynamic subject token with valid expiry is preserved in after_run_callback."""
        import time

        # Create a token that expires in 1 hour
        future_expiry = int(time.time()) + 3600
        jwt_token = "header.payload.signature"  # Mock JWT

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = Mock(return_value="dynamic-actor")
        sts.exchange_token = AsyncMock(return_value=jwt_token)

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-9", headers={"Authorization": "Bearer subject"})

        # Mock the expiry extraction to return future timestamp
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            await plugin.before_run_callback(invocation_context=ic)

        # Verify token is cached with expiry
        assert "sess-9" in plugin.token_cache
        cache_entry = plugin.token_cache["sess-9"]
        assert cache_entry.expiry == future_expiry

        # Call after_run_callback - token should be preserved
        await plugin.after_run_callback(invocation_context=ic)

        # Verify token is still cached (not expired)
        assert "sess-9" in plugin.token_cache

    @pytest.mark.asyncio
    async def test_dynamic_token_removed_when_expired(self):
        """Case: dynamic token with past expiry is removed in after_run_callback."""
        import time

        # Create a token that expired 1 hour ago
        past_expiry = int(time.time()) - 3600
        jwt_token = "header.payload.signature"  # Mock JWT

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = Mock(return_value="dynamic-actor")
        sts.exchange_token = AsyncMock(return_value=jwt_token)

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-10", headers={"Authorization": "Bearer subject"})

        # Mock the expiry extraction to return past timestamp
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=past_expiry):
            await plugin.before_run_callback(invocation_context=ic)

        # Verify token is cached
        assert "sess-10" in plugin.token_cache

        # Call after_run_callback - token should be removed (expired)
        await plugin.after_run_callback(invocation_context=ic)

        # Verify token is removed
        assert "sess-10" not in plugin.token_cache

    @pytest.mark.asyncio
    async def test_valid_token_preserved_in_cache(self):
        """Case: valid token (not expired) is preserved in after_run_callback."""
        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = None
        sts._actor_token = "static-actor"
        sts.exchange_token = AsyncMock(return_value="access-token-static")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-11", headers={"Authorization": "Bearer subject"})

        await plugin.before_run_callback(invocation_context=ic)

        # Verify token is cached with expected value
        assert "sess-11" in plugin.token_cache
        cache_entry = plugin.token_cache["sess-11"]
        assert cache_entry.token == "access-token-static"

        # Call after_run_callback - token should still be in cache if not expired
        await plugin.after_run_callback(invocation_context=ic)

        # Verify the same cache entry is still present
        assert plugin.token_cache.get("sess-11") is cache_entry

    @pytest.mark.asyncio
    async def test_actor_token_cached_and_reused(self):
        """Case: actor token is cached on first fetch and reused on subsequent calls."""
        import time

        future_expiry = int(time.time()) + 3600
        fetch_count = 0

        def sync_fetch_token():
            nonlocal fetch_count
            fetch_count += 1
            return "dynamic-actor-token"

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = sync_fetch_token
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token")

        plugin = ADKTokenPropagationPlugin(sts)

        # Mock expiry extraction for both actor and subject tokens
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            # First call - should fetch actor token
            ic1 = self._make_invocation_context("sess-12a", headers={"Authorization": "Bearer subject1"})
            await plugin.before_run_callback(invocation_context=ic1)

            assert fetch_count == 1
            assert plugin.actor_token_cache is not None
            assert plugin.actor_token_cache.token == "dynamic-actor-token"
            assert plugin.actor_token_cache.expiry == future_expiry

            # Second call - should reuse cached actor token
            ic2 = self._make_invocation_context("sess-12b", headers={"Authorization": "Bearer subject2"})
            await plugin.before_run_callback(invocation_context=ic2)

            # Fetch should not be called again
            assert fetch_count == 1

    @pytest.mark.asyncio
    async def test_actor_token_cache_expired_and_refetched(self):
        """Case: expired actor token is removed from cache and refetched."""
        import time

        past_expiry = int(time.time()) - 100
        future_expiry = int(time.time()) + 3600
        fetch_count = 0

        def sync_fetch_token():
            nonlocal fetch_count
            fetch_count += 1
            return f"dynamic-actor-token-{fetch_count}"

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = sync_fetch_token
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token")

        plugin = ADKTokenPropagationPlugin(sts)

        # First call - cache with expired token
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=past_expiry):
            ic1 = self._make_invocation_context("sess-13a", headers={"Authorization": "Bearer subject1"})
            await plugin.before_run_callback(invocation_context=ic1)

            assert fetch_count == 1
            assert plugin.actor_token_cache.token == "dynamic-actor-token-1"
            assert plugin.actor_token_cache.expiry == past_expiry

        # Second call - should detect expiry and refetch
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            ic2 = self._make_invocation_context("sess-13b", headers={"Authorization": "Bearer subject2"})
            await plugin.before_run_callback(invocation_context=ic2)

            # Should have fetched again
            assert fetch_count == 2
            assert plugin.actor_token_cache.token == "dynamic-actor-token-2"
            assert plugin.actor_token_cache.expiry == future_expiry

    @pytest.mark.asyncio
    async def test_actor_token_cache_cleanup_on_expiry(self):
        """Case: expired actor token is removed from cache in after_run_callback."""
        import time

        past_expiry = int(time.time()) - 100

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = Mock(return_value="dynamic-actor")
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-14", headers={"Authorization": "Bearer subject"})

        # Setup with expired actor token
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=past_expiry):
            await plugin.before_run_callback(invocation_context=ic)

        assert plugin.actor_token_cache is not None

        # Call after_run_callback - should remove expired actor token
        with patch("agentsts.adk._base.logger") as mock_logger:
            await plugin.after_run_callback(invocation_context=ic)

            # Verify actor token cache is cleared
            assert plugin.actor_token_cache is None

            # Verify debug log
            debug_calls = [call.args[0] for call in mock_logger.debug.call_args_list]
            assert any("Removing expired actor token" in call for call in debug_calls)

    @pytest.mark.asyncio
    async def test_actor_token_cache_preserved_when_not_expired(self):
        """Case: valid actor token is preserved in after_run_callback."""
        import time

        future_expiry = int(time.time()) + 3600

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = Mock(return_value="dynamic-actor")
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-15", headers={"Authorization": "Bearer subject"})

        # Setup with valid actor token
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            await plugin.before_run_callback(invocation_context=ic)

        assert plugin.actor_token_cache is not None
        original_cache = plugin.actor_token_cache

        # Call after_run_callback - should preserve valid actor token
        await plugin.after_run_callback(invocation_context=ic)

        # Verify actor token cache is still present
        assert plugin.actor_token_cache is original_cache
        assert plugin.actor_token_cache.token == "dynamic-actor"

    @pytest.mark.asyncio
    async def test_actor_token_no_expiry_cached_indefinitely(self):
        """Case: actor token without expiry claim is cached indefinitely."""
        fetch_count = 0

        def sync_fetch_token():
            nonlocal fetch_count
            fetch_count += 1
            return "actor-token-no-expiry"

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = sync_fetch_token
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token")

        plugin = ADKTokenPropagationPlugin(sts)

        # Mock expiry extraction to return None (no expiry)
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=None):
            # First call
            ic1 = self._make_invocation_context("sess-16a", headers={"Authorization": "Bearer subject1"})
            await plugin.before_run_callback(invocation_context=ic1)

            assert fetch_count == 1
            assert plugin.actor_token_cache.expiry is None

            # after_run_callback should preserve it
            await plugin.after_run_callback(invocation_context=ic1)
            assert plugin.actor_token_cache is not None

            # Second call - should reuse cached token (no expiry check)
            ic2 = self._make_invocation_context("sess-16b", headers={"Authorization": "Bearer subject2"})
            await plugin.before_run_callback(invocation_context=ic2)

            # Should not fetch again
            assert fetch_count == 1

    @pytest.mark.asyncio
    async def test_subject_token_cached_and_reused(self):
        """Case: subject token is cached and reused on subsequent calls, skipping STS exchange."""
        import time

        future_expiry = int(time.time()) + 3600

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = None
        sts._actor_token = "static-actor"
        sts.exchange_token = AsyncMock(return_value="exchanged-token")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-18", headers={"Authorization": "Bearer subject-token"})

        # Mock expiry extraction
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            # First call - should exchange token
            await plugin.before_run_callback(invocation_context=ic)

            # Verify exchange was called once
            assert sts.exchange_token.call_count == 1

            # Verify token is cached
            assert "sess-18" in plugin.token_cache
            assert plugin.token_cache["sess-18"].token == "exchanged-token"

            # Second call with same session - should use cached token
            await plugin.before_run_callback(invocation_context=ic)

            # Verify exchange was NOT called again
            assert sts.exchange_token.call_count == 1

    @pytest.mark.asyncio
    async def test_subject_token_reexchanged_after_expiry(self):
        """Case: expired subject token is removed and re-exchanged on next call."""
        import time

        past_expiry = int(time.time()) - 100
        future_expiry = int(time.time()) + 3600

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = None
        sts._actor_token = "static-actor"
        sts.exchange_token = AsyncMock(side_effect=["token-1", "token-2"])

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-19", headers={"Authorization": "Bearer subject-token"})

        # First call - cache with expired token
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=past_expiry):
            await plugin.before_run_callback(invocation_context=ic)
            assert sts.exchange_token.call_count == 1
            assert plugin.token_cache["sess-19"].token == "token-1"
            assert plugin.token_cache["sess-19"].expiry == past_expiry

            # Cleanup expired token
            await plugin.after_run_callback(invocation_context=ic)
            assert "sess-19" not in plugin.token_cache

        # Second call - should detect missing cache and re-exchange
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            await plugin.before_run_callback(invocation_context=ic)

            # Verify exchange was called again
            assert sts.exchange_token.call_count == 2
            assert plugin.token_cache["sess-19"].token == "token-2"
            assert plugin.token_cache["sess-19"].expiry == future_expiry

    @pytest.mark.asyncio
    async def test_subject_token_cache_no_expiry(self):
        """Case: subject token without expiry is cached indefinitely and reused."""
        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = None
        sts._actor_token = "static-actor"
        sts.exchange_token = AsyncMock(return_value="exchanged-token-no-exp")

        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-20", headers={"Authorization": "Bearer subject-token"})

        # Mock expiry extraction to return None
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=None):
            # First call
            await plugin.before_run_callback(invocation_context=ic)
            assert sts.exchange_token.call_count == 1
            assert plugin.token_cache["sess-20"].expiry is None

            # after_run_callback should preserve it (no expiry)
            await plugin.after_run_callback(invocation_context=ic)
            assert "sess-20" in plugin.token_cache

            # Second call - should reuse cached token
            await plugin.before_run_callback(invocation_context=ic)
            # Exchange should not be called again
            assert sts.exchange_token.call_count == 1

    @pytest.mark.asyncio
    async def test_actor_token_cached_and_reused_async(self):
        """Case: async actor token is cached on first fetch and reused on subsequent calls."""
        import time

        future_expiry = int(time.time()) + 3600
        fetch_count = 0

        async def async_fetch_token():
            nonlocal fetch_count
            fetch_count += 1
            return "dynamic-actor-token-async"

        sts = Mock(spec=ADKSTSIntegration)
        sts.fetch_actor_token = async_fetch_token
        sts._actor_token = None
        sts.exchange_token = AsyncMock(return_value="access-token")

        plugin = ADKTokenPropagationPlugin(sts)

        # Mock expiry extraction
        with patch("agentsts.adk._base._extract_jwt_expiry", return_value=future_expiry):
            # First call - should fetch actor token
            ic1 = self._make_invocation_context("sess-17a", headers={"Authorization": "Bearer subject1"})
            await plugin.before_run_callback(invocation_context=ic1)

            assert fetch_count == 1
            assert plugin.actor_token_cache is not None
            assert plugin.actor_token_cache.token == "dynamic-actor-token-async"

            # Second call - should reuse cached actor token
            ic2 = self._make_invocation_context("sess-17b", headers={"Authorization": "Bearer subject2"})
            await plugin.before_run_callback(invocation_context=ic2)

            # Fetch should not be called again
            assert fetch_count == 1

    def test_extract_jwt_from_headers_success(self):
        """Test successful JWT extraction from headers."""
        headers = {"Authorization": "Bearer jwt-token-123"}

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers(headers)

            assert result == "jwt-token-123"
            mock_logger.debug.assert_called_once()

    def test_extract_jwt_from_headers_no_headers(self):
        """Test JWT extraction with no headers."""
        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers({})

            assert result is None
            mock_logger.warning.assert_called_once_with("No headers provided for JWT extraction")

    def test_extract_jwt_from_headers_no_auth_header(self):
        """Test JWT extraction with no Authorization header."""
        headers = {"Other-Header": "value"}

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers(headers)

            assert result is None
            mock_logger.warning.assert_called_once_with("No Authorization header found in request")

    def test_extract_jwt_from_headers_invalid_bearer(self):
        """Test JWT extraction with invalid Bearer format."""
        headers = {"Authorization": "Basic jwt-token-123"}

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers(headers)

            assert result is None
            mock_logger.warning.assert_called_once_with("Authorization header must start with Bearer")

    def test_extract_jwt_from_headers_empty_token(self):
        """Test JWT extraction with empty token."""
        headers = {"Authorization": "Bearer "}

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers(headers)

            assert result is None
            mock_logger.warning.assert_called_once_with("Empty JWT token found in Authorization header")

    def test_extract_jwt_from_headers_whitespace_token(self):
        """Test JWT extraction with whitespace-only token."""
        headers = {"Authorization": "Bearer   \n\t  "}

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers(headers)

            assert result is None
            mock_logger.warning.assert_called_once_with("Empty JWT token found in Authorization header")

    def test_extract_jwt_from_headers_stripped_token(self):
        """Test JWT extraction with token that has whitespace."""
        headers = {"Authorization": "Bearer  jwt-token-123  \n"}

        with patch("agentsts.adk._base.logger") as mock_logger:
            result = extract_jwt_from_headers(headers)

            assert result == "jwt-token-123"
            mock_logger.debug.assert_called_once()

    def test_extract_jwt_expiry_success(self):
        """Test successful JWT expiry extraction."""
        import time

        expiry = int(time.time()) + 3600
        # Create a mock JWT token with expiry claim
        mock_decoded = {"exp": expiry, "sub": "user123"}

        with patch("jwt.decode", return_value=mock_decoded):
            result = extract_jwt_expiry("mock.jwt.token")

            assert result == expiry

    def test_extract_jwt_expiry_no_exp_claim(self):
        """Test JWT expiry extraction when 'exp' claim is missing."""
        # Create a mock JWT token without expiry claim
        mock_decoded = {"sub": "user123", "iat": 1234567890}

        with patch("jwt.decode", return_value=mock_decoded):
            with patch("agentsts.adk._base.logger") as mock_logger:
                result = extract_jwt_expiry("mock.jwt.token")

                assert result is None
                debug_calls = [call.args[0] for call in mock_logger.debug.call_args_list]
                assert any("No 'exp' claim found" in call for call in debug_calls)

    def test_extract_jwt_expiry_invalid_token(self):
        """Test JWT expiry extraction with invalid token."""
        with patch("jwt.decode", side_effect=Exception("Invalid token")):
            with patch("agentsts.adk._base.logger") as mock_logger:
                result = extract_jwt_expiry("invalid-token")

                assert result is None
                mock_logger.warning.assert_called_once()
                warning_msg = mock_logger.warning.call_args[0][0]
                assert "Failed to extract JWT expiry" in warning_msg

    def test_has_token_expired_with_future_expiry(self):
        """Test token expiration check with future expiry (beyond buffer)."""
        import time

        # Token expires in 1 hour (well beyond 5 second buffer)
        future_expiry = int(time.time()) + 3600
        assert has_token_expired(future_expiry) is False

    def test_has_token_expired_with_past_expiry(self):
        """Test token expiration check with past expiry."""
        import time

        past_expiry = int(time.time()) - 100
        assert has_token_expired(past_expiry) is True

    def test_has_token_expired_with_exact_current_time(self):
        """Test token expiration check with exact current time (edge case)."""
        import time

        current_time = int(time.time())
        # Token expires at current time means it's expired (<=)
        assert has_token_expired(current_time) is True

    def test_has_token_expired_with_no_expiry(self):
        """Test token expiration check with None expiry."""
        assert has_token_expired(None) is False

    def test_has_token_expired_with_zero_expiry(self):
        """Test token expiration check with zero expiry."""
        # 0 implies expired
        assert has_token_expired(0) is True

    def test_has_token_expired_with_buffer_expiring_soon(self):
        """Test token expiration check with token expiring within buffer window."""
        import time

        # Token expires in 3 seconds (within default 5 second buffer)
        soon_expiry = int(time.time()) + 3
        assert has_token_expired(soon_expiry) is True

    def test_has_token_expired_with_buffer_at_boundary(self):
        """Test token expiration check at exact buffer boundary."""
        import time

        # Token expires in exactly 5 seconds (at buffer boundary)
        boundary_expiry = int(time.time()) + 5
        assert has_token_expired(boundary_expiry) is True

    def test_has_token_expired_with_buffer_just_beyond(self):
        """Test token expiration check just beyond buffer window."""
        import time

        # Token expires in 6 seconds (just beyond 5 second buffer)
        beyond_buffer_expiry = int(time.time()) + 6
        assert has_token_expired(beyond_buffer_expiry) is False

    def test_has_token_expired_with_custom_buffer(self):
        """Test token expiration check with custom buffer time."""
        import time

        # Token expires in 8 seconds
        expiry = int(time.time()) + 8

        # With default 5 second buffer - not expired
        assert has_token_expired(expiry, buffer_seconds=5) is False

        # With 10 second buffer - expired
        assert has_token_expired(expiry, buffer_seconds=10) is True

    def test_has_token_expired_with_zero_buffer(self):
        """Test token expiration check with zero buffer (exact expiry check)."""
        import time

        current_time = int(time.time())

        # Token expires in 1 second
        expiry = current_time + 1

        # With 0 buffer - not expired yet
        assert has_token_expired(expiry, buffer_seconds=0) is False

        # With default buffer - expired (within 5 seconds)
        assert has_token_expired(expiry) is True

    def test_expiry_end_to_end_with_real_jwt(self):
        """Verify _extract_jwt_expiry + _has_token_expired work correctly with a real JWT."""
        import time

        import jwt as pyjwt

        future_exp = int(time.time()) + 3600
        past_exp = int(time.time()) - 100

        valid_token = pyjwt.encode({"exp": future_exp, "sub": "user"}, "secret", algorithm="HS256")
        expired_token = pyjwt.encode({"exp": past_exp, "sub": "user"}, "secret", algorithm="HS256")
        no_exp_token = pyjwt.encode({"sub": "user"}, "secret", algorithm="HS256")

        assert extract_jwt_expiry(valid_token) == future_exp
        assert has_token_expired(extract_jwt_expiry(valid_token)) is False

        assert extract_jwt_expiry(expired_token) == past_exp
        assert has_token_expired(extract_jwt_expiry(expired_token)) is True

        assert extract_jwt_expiry(no_exp_token) is None
        assert has_token_expired(extract_jwt_expiry(no_exp_token)) is False


class TestADKSTSIntegration:
    """Test cases for ADKSTSIntegration."""

    @pytest.mark.asyncio
    async def test_get_auth_credential_with_actor_token(self):
        """Test that get_auth_credential calls exchange_token with actor token."""
        adk_integration = ADKSTSIntegration("https://example.com/.well-known/oauth-authorization-server")
        adk_integration._actor_token = "system:serviceaccount:default:example-agent"
        response = TokenExchangeResponse(
            access_token="mock-auth-credential",
            issued_token_type=TokenType.JWT,
        )
        adk_integration.sts_client.exchange_token = AsyncMock(return_value=response)

        result = await adk_integration.exchange_token(
            subject_token="mock-subject-token",
            subject_token_type=TokenType.JWT,
            actor_token="mock-actor-token",
            actor_token_type=TokenType.JWT,
        )

        # Verify exchange_token was called with actor token
        adk_integration.sts_client.exchange_token.assert_called_once_with(
            subject_token="mock-subject-token",
            subject_token_type=TokenType.JWT,
            actor_token="mock-actor-token",
            actor_token_type=TokenType.JWT,
            additional_parameters=None,
            audience=None,
            resource=None,
            requested_token_type=None,
            scope=None,
        )

        assert result == "mock-auth-credential"

    def test_init_with_fetch_actor_token_sync(self):
        """Test that ADKSTSIntegration initializes correctly with sync fetch_actor_token."""
        fetch_token_mock = Mock(return_value="dynamic-token-123")

        with patch("agentsts.core._base.ActorTokenService") as mock_service:
            adk_integration = ADKSTSIntegration(
                well_known_uri="https://example.com/.well-known/oauth-authorization-server",
                fetch_actor_token=fetch_token_mock,
            )

            # ActorTokenService should not be called when fetch_actor_token is provided
            mock_service.assert_not_called()

            # _actor_token should be None (will be fetched dynamically)
            assert adk_integration._actor_token is None
            assert adk_integration.fetch_actor_token is fetch_token_mock

    def test_init_with_fetch_actor_token_async(self):
        """Test that ADKSTSIntegration initializes correctly with async fetch_actor_token."""

        async def async_fetch_token():
            return "dynamic-token-async"

        with patch("agentsts.core._base.ActorTokenService") as mock_service:
            adk_integration = ADKSTSIntegration(
                well_known_uri="https://example.com/.well-known/oauth-authorization-server",
                fetch_actor_token=async_fetch_token,
            )

            # ActorTokenService should not be called when fetch_actor_token is provided
            mock_service.assert_not_called()

            # _actor_token should be None (will be fetched dynamically)
            assert adk_integration._actor_token is None
            assert adk_integration.fetch_actor_token is async_fetch_token

    def test_init_with_service_account_path(self):
        """Test that ADKSTSIntegration initializes correctly with service_account_token_path."""
        with patch("agentsts.core._base.ActorTokenService") as mock_service:
            mock_service.return_value.get_actor_token.return_value = "static-token-456"

            adk_integration = ADKSTSIntegration(
                well_known_uri="https://example.com/.well-known/oauth-authorization-server",
                service_account_token_path="/path/to/token",
            )

            # ActorTokenService should be called when service_account_token_path is provided
            mock_service.assert_called_once_with("/path/to/token")
            assert adk_integration._actor_token == "static-token-456"
            assert adk_integration.fetch_actor_token is None

    def test_init_with_use_issuer_host(self):
        """Test that ADKSTSIntegration passes use_issuer_host to STSConfig."""
        with patch("agentsts.core._base.ActorTokenService"):
            integration = ADKSTSIntegration(
                well_known_uri="http://192.168.1.100:7777/.well-known/oauth-authorization-server",
                use_issuer_host=True,
            )
            assert integration.sts_client.config.use_issuer_host is True

    def test_init_without_use_issuer_host_defaults_to_false(self):
        """Test that use_issuer_host defaults to False."""
        with patch("agentsts.core._base.ActorTokenService"):
            integration = ADKSTSIntegration(
                well_known_uri="https://example.com/.well-known/oauth-authorization-server",
            )
            assert integration.sts_client.config.use_issuer_host is False

    @pytest.mark.asyncio
    async def test_use_issuer_host_replaces_token_endpoint_host(self):
        """Test that use_issuer_host replaces host:port in token_endpoint with the issuer host."""
        well_known_uri = "http://192.168.1.100:7777/.well-known/oauth-authorization-server"
        well_known_response = {
            "token_endpoint": "foo.bar:7777/oauth2/token",
            "issuer": "http://192.168.1.100:7777",
        }

        with patch("agentsts.core._base.ActorTokenService"):
            integration = ADKSTSIntegration(
                well_known_uri=well_known_uri,
                use_issuer_host=True,
            )

        with patch("agentsts.core.client._utils.httpx.AsyncClient") as mock_client_cls:
            mock_response = Mock()
            mock_response.json.return_value = well_known_response
            mock_response.raise_for_status = Mock()
            mock_client_cls.return_value.__aenter__ = AsyncMock(return_value=mock_client_cls.return_value)
            mock_client_cls.return_value.__aexit__ = AsyncMock(return_value=False)
            mock_client_cls.return_value.get = AsyncMock(return_value=mock_response)

            await integration.sts_client._initialize()

        assert integration.sts_client._well_known_config.token_endpoint == "http://192.168.1.100:7777/oauth2/token"

    @pytest.mark.asyncio
    async def test_use_issuer_host_preserves_existing_scheme_in_token_endpoint(self):
        """Test that use_issuer_host keeps the scheme from token_endpoint when it already has one."""
        well_known_uri = "http://192.168.1.100:7777/.well-known/oauth-authorization-server"
        well_known_response = {
            "token_endpoint": "https://foo.bar:7777/oauth2/token",
            "issuer": "http://192.168.1.100:7777",
        }

        with patch("agentsts.core._base.ActorTokenService"):
            integration = ADKSTSIntegration(
                well_known_uri=well_known_uri,
                use_issuer_host=True,
            )

        with patch("agentsts.core.client._utils.httpx.AsyncClient") as mock_client_cls:
            mock_response = Mock()
            mock_response.json.return_value = well_known_response
            mock_response.raise_for_status = Mock()
            mock_client_cls.return_value.__aenter__ = AsyncMock(return_value=mock_client_cls.return_value)
            mock_client_cls.return_value.__aexit__ = AsyncMock(return_value=False)
            mock_client_cls.return_value.get = AsyncMock(return_value=mock_response)

            await integration.sts_client._initialize()

        # https from token_endpoint is preserved; only host is replaced
        assert integration.sts_client._well_known_config.token_endpoint == "https://192.168.1.100:7777/oauth2/token"

    @pytest.mark.asyncio
    async def test_get_auth_credential_without_actor_token(self):
        """Test that get_auth_credential calls exchange_token without actor token when none is set."""
        adk_integration = ADKSTSIntegration("https://example.com/.well-known/oauth-authorization-server")
        adk_integration._actor_token = None
        adk_integration._actor_token = "system:serviceaccount:default:example-agent"
        response = TokenExchangeResponse(
            access_token="mock-auth-credential",
            issued_token_type=TokenType.JWT,
        )
        adk_integration.sts_client.exchange_token = AsyncMock(return_value=response)

        result = await adk_integration.exchange_token(
            subject_token="mock-subject-token",
            subject_token_type=TokenType.JWT,
            actor_token=None,
            actor_token_type=None,
        )

        # Verify exchange_token was called with actor token
        adk_integration.sts_client.exchange_token.assert_called_once_with(
            subject_token="mock-subject-token",
            subject_token_type=TokenType.JWT,
            actor_token=None,
            actor_token_type=None,
            additional_parameters=None,
            audience=None,
            resource=None,
            requested_token_type=None,
            scope=None,
        )

        assert result == "mock-auth-credential"
