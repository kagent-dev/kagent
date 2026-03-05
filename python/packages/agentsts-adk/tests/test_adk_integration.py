"""Tests for ADK integration classes (STS + token propagation)."""

from unittest.mock import AsyncMock, Mock, patch

import pytest
from google.adk.agents import LlmAgent
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.mcp_tool.mcp_toolset import MCPToolset

from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from agentsts.adk._base import HEADERS_KEY
from agentsts.adk._base import _extract_jwt_from_headers as extract_jwt_from_headers
from agentsts.core import TokenType
from agentsts.core.client import TokenExchangeResponse


class TestADKTokenPropagationPlugin:
    """Unit tests for token propagation plugin covering: none, downstream, STS exchange, and audience scoping."""

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

    def _make_tool_context(self, invocation_context):
        tool_context = Mock()
        tool_context._invocation_context = invocation_context
        return tool_context

    def _make_mcp_tool(self, session_manager=None):
        tool = Mock(spec=MCPTool)
        if session_manager is not None:
            tool._mcp_session_manager = session_manager
        return tool

    def test_init(self):
        mock_sts_integration = Mock()
        plugin = ADKTokenPropagationPlugin(mock_sts_integration)
        assert plugin.name == "ADKTokenPropagationPlugin"
        assert plugin.sts_integration is mock_sts_integration
        assert plugin.token_cache == {}
        assert plugin._subject_tokens == {}
        assert plugin._audience_map == {}

    def test_register_toolset(self):
        """Registering a toolset maps its session manager id to audience."""
        plugin = ADKTokenPropagationPlugin()
        toolset = Mock(spec=MCPToolset)
        session_manager = Mock()
        toolset._mcp_session_manager = session_manager

        plugin.register_toolset(toolset, "https://audience.example.com")
        assert plugin._audience_map[id(session_manager)] == "https://audience.example.com"

    def test_register_toolset_no_audience(self):
        """Registering with None or empty audience is a no-op."""
        plugin = ADKTokenPropagationPlugin()
        toolset = Mock(spec=MCPToolset)
        toolset._mcp_session_manager = Mock()

        plugin.register_toolset(toolset, None)
        assert plugin._audience_map == {}

        plugin.register_toolset(toolset, "")
        assert plugin._audience_map == {}

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
        assert plugin._subject_tokens == {}

    @pytest.mark.asyncio
    async def test_downstream_token_propagation_without_sts(self):
        """Case: headers present, no STS integration -> subject token cached and available via header_provider."""
        plugin = ADKTokenPropagationPlugin(sts_integration=None)
        ic = self._make_invocation_context("sess-2", headers={"Authorization": "Bearer subj-token-123"})
        result = await plugin.before_run_callback(invocation_context=ic)
        assert result is None
        # Subject token stored for later
        assert plugin._subject_tokens["sess-2"] == "subj-token-123"
        # Without STS, subject token is directly cached under session key
        assert plugin.token_cache["sess-2"] == "subj-token-123"

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

        # cleanup
        await plugin.after_run_callback(invocation_context=ic)
        assert "sess-2" not in plugin.token_cache
        assert "sess-2" not in plugin._subject_tokens

    @pytest.mark.asyncio
    async def test_before_tool_callback_with_sts_exchange(self):
        """Case: STS integration exchanges token on first MCP tool call."""
        sts = Mock(spec=ADKSTSIntegration)
        sts._actor_token = "actor-token"
        sts.exchange_token = AsyncMock(return_value="access-token-XYZ")
        plugin = ADKTokenPropagationPlugin(sts)

        ic = self._make_invocation_context("sess-3", headers={"Authorization": "Bearer original-subject"})
        # before_run stores subject token
        await plugin.before_run_callback(invocation_context=ic)
        assert plugin._subject_tokens["sess-3"] == "original-subject"
        # No exchange yet (STS present, exchange deferred to before_tool_callback)
        assert plugin.token_cache == {}

        # before_tool_callback triggers exchange
        session_manager = Mock()
        tool = self._make_mcp_tool(session_manager=session_manager)
        tc = self._make_tool_context(ic)

        result = await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tc)
        assert result is None
        sts.exchange_token.assert_called_once_with(
            subject_token="original-subject",
            subject_token_type=TokenType.JWT,
            actor_token="actor-token",
            actor_token_type=TokenType.JWT,
            audience=None,  # no audience registered for this tool
        )
        assert plugin.token_cache["sess-3"] == "access-token-XYZ"

        ro_ctx = self._make_readonly_context(ic)
        headers = plugin.header_provider(ro_ctx)
        assert headers == {"Authorization": "Bearer access-token-XYZ"}

        await plugin.after_run_callback(invocation_context=ic)
        assert "sess-3" not in plugin.token_cache

    @pytest.mark.asyncio
    async def test_before_tool_callback_with_audience(self):
        """Case: STS exchange with audience-scoped token."""
        sts = Mock(spec=ADKSTSIntegration)
        sts._actor_token = "actor-token"
        sts.exchange_token = AsyncMock(return_value="scoped-token-ABC")
        plugin = ADKTokenPropagationPlugin(sts)

        # Register a toolset with an audience
        session_manager = Mock()
        toolset = Mock(spec=MCPToolset)
        toolset._mcp_session_manager = session_manager
        plugin.register_toolset(toolset, "https://my-service.example.com")

        ic = self._make_invocation_context("sess-aud", headers={"Authorization": "Bearer subj"})
        await plugin.before_run_callback(invocation_context=ic)

        # Tool using that session_manager
        tool = self._make_mcp_tool(session_manager=session_manager)
        tc = self._make_tool_context(ic)

        result = await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tc)
        assert result is None
        sts.exchange_token.assert_called_once_with(
            subject_token="subj",
            subject_token_type=TokenType.JWT,
            actor_token="actor-token",
            actor_token_type=TokenType.JWT,
            audience="https://my-service.example.com",
        )
        assert plugin.token_cache["sess-aud:https://my-service.example.com"] == "scoped-token-ABC"

        # header_provider with audience returns scoped token
        ro_ctx = self._make_readonly_context(ic)
        headers = plugin.header_provider(ro_ctx, audience="https://my-service.example.com")
        assert headers == {"Authorization": "Bearer scoped-token-ABC"}

    @pytest.mark.asyncio
    async def test_per_audience_cache_isolation(self):
        """Tools with different audiences get different tokens."""
        call_count = 0

        async def mock_exchange(**kwargs):
            nonlocal call_count
            call_count += 1
            audience = kwargs.get("audience", "")
            return f"token-for-{audience}-{call_count}"

        sts = Mock(spec=ADKSTSIntegration)
        sts._actor_token = "actor"
        sts.exchange_token = AsyncMock(side_effect=mock_exchange)
        plugin = ADKTokenPropagationPlugin(sts)

        # Two toolsets with different audiences
        sm_a = Mock()
        toolset_a = Mock(spec=MCPToolset)
        toolset_a._mcp_session_manager = sm_a
        plugin.register_toolset(toolset_a, "audience-A")

        sm_b = Mock()
        toolset_b = Mock(spec=MCPToolset)
        toolset_b._mcp_session_manager = sm_b
        plugin.register_toolset(toolset_b, "audience-B")

        ic = self._make_invocation_context("sess-iso", headers={"Authorization": "Bearer subj"})
        await plugin.before_run_callback(invocation_context=ic)

        tc = self._make_tool_context(ic)

        # First tool call (audience A)
        tool_a = self._make_mcp_tool(session_manager=sm_a)
        await plugin.before_tool_callback(tool=tool_a, tool_args={}, tool_context=tc)
        assert plugin.token_cache["sess-iso:audience-A"] == "token-for-audience-A-1"

        # Second tool call (audience B)
        tool_b = self._make_mcp_tool(session_manager=sm_b)
        await plugin.before_tool_callback(tool=tool_b, tool_args={}, tool_context=tc)
        assert plugin.token_cache["sess-iso:audience-B"] == "token-for-audience-B-2"

        # Repeat call for audience A should NOT re-exchange (cached)
        await plugin.before_tool_callback(tool=tool_a, tool_args={}, tool_context=tc)
        assert sts.exchange_token.call_count == 2  # still only 2 calls

        # header_provider returns correct token per audience
        ro_ctx = self._make_readonly_context(ic)
        assert plugin.header_provider(ro_ctx, audience="audience-A") == {"Authorization": "Bearer token-for-audience-A-1"}
        assert plugin.header_provider(ro_ctx, audience="audience-B") == {"Authorization": "Bearer token-for-audience-B-2"}

    @pytest.mark.asyncio
    async def test_before_tool_callback_skips_non_mcp_tools(self):
        """Non-MCPTool tools are skipped."""
        sts = Mock(spec=ADKSTSIntegration)
        plugin = ADKTokenPropagationPlugin(sts)

        ic = self._make_invocation_context("sess-skip", headers={"Authorization": "Bearer subj"})
        await plugin.before_run_callback(invocation_context=ic)

        non_mcp_tool = Mock()  # not an MCPTool
        tc = self._make_tool_context(ic)
        result = await plugin.before_tool_callback(tool=non_mcp_tool, tool_args={}, tool_context=tc)
        assert result is None
        assert plugin.token_cache == {}  # no exchange happened

    @pytest.mark.asyncio
    async def test_before_tool_callback_no_sts(self):
        """Without STS integration, before_tool_callback is a no-op."""
        plugin = ADKTokenPropagationPlugin(sts_integration=None)

        ic = self._make_invocation_context("sess-no-sts", headers={"Authorization": "Bearer subj"})
        await plugin.before_run_callback(invocation_context=ic)

        tool = self._make_mcp_tool(session_manager=Mock())
        tc = self._make_tool_context(ic)
        result = await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tc)
        assert result is None

    @pytest.mark.asyncio
    async def test_sts_token_exchange_failure(self):
        """Case: STS exchange raises -> no cache entry, graceful warning."""
        sts = Mock(spec=ADKSTSIntegration)
        sts._actor_token = "actor-token"
        sts.exchange_token = AsyncMock(side_effect=Exception("boom"))
        plugin = ADKTokenPropagationPlugin(sts)
        ic = self._make_invocation_context("sess-4", headers={"Authorization": "Bearer original-subject"})
        await plugin.before_run_callback(invocation_context=ic)

        tool = self._make_mcp_tool(session_manager=Mock())
        tc = self._make_tool_context(ic)
        with patch("agentsts.adk._base.logger") as mock_logger:
            result = await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tc)
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
        assert plugin.header_provider(ro_ctx) == {}

    @pytest.mark.asyncio
    async def test_after_run_callback_removes_all_audience_tokens(self):
        """after_run_callback removes all cached tokens for the session across audiences."""
        plugin = ADKTokenPropagationPlugin()
        ic = self._make_invocation_context("sess-6", headers={"Authorization": "Bearer AAA"})
        # Simulate multiple cached tokens for different audiences
        plugin.token_cache["sess-6"] = "token-default"
        plugin.token_cache["sess-6:aud-A"] = "token-A"
        plugin.token_cache["sess-6:aud-B"] = "token-B"
        plugin.token_cache["sess-7:aud-A"] = "other-session-token"
        plugin._subject_tokens["sess-6"] = "AAA"

        await plugin.after_run_callback(invocation_context=ic)

        # All sess-6 entries removed (both bare key and audience-scoped keys)
        assert "sess-6" not in plugin.token_cache
        assert not any(k.startswith("sess-6:") for k in plugin.token_cache)
        assert "sess-6" not in plugin._subject_tokens
        # Other sessions untouched
        assert plugin.token_cache["sess-7:aud-A"] == "other-session-token"

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
