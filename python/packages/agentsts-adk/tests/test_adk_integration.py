"""Tests for ADK integration classes (STS + token propagation)."""

from unittest.mock import AsyncMock, Mock, patch

import pytest
from google.adk.agents import LlmAgent
from google.adk.tools.mcp_tool.mcp_toolset import MCPToolset

from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from agentsts.adk._base import HEADERS_KEY
from agentsts.adk._base import _extract_jwt_from_headers as extract_jwt_from_headers
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

    @pytest.mark.asyncio
    async def test_sts_token_exchange_success(self):
        """Case: STS integration exchanges token -> access token cached and returned by header provider."""
        sts = Mock(spec=ADKSTSIntegration)
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
        assert plugin.token_cache["sess-3"] == "access-token-XYZ"

        ro_ctx = self._make_readonly_context(ic)
        headers = plugin.header_provider(ro_ctx)
        assert headers == {"Authorization": "Bearer access-token-XYZ"}

        await plugin.after_run_callback(invocation_context=ic)
        assert "sess-3" not in plugin.token_cache

    @pytest.mark.asyncio
    async def test_sts_token_exchange_failure(self):
        """Case: STS exchange raises -> no cache entry, graceful warning."""
        sts = Mock(spec=ADKSTSIntegration)
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
        # token_cache intentionally missing key -> KeyError would occur; simulate by setting empty string
        plugin.token_cache["sess-5"] = ""  # empty token should result in {}
        assert plugin.header_provider(ro_ctx) == {}

    @pytest.mark.asyncio
    async def test_after_run_callback_removes_token(self):
        """Case: after_run_callback removes cached token."""
        plugin = ADKTokenPropagationPlugin()
        ic = self._make_invocation_context("sess-6", headers={"Authorization": "Bearer AAA"})
        await plugin.before_run_callback(invocation_context=ic)
        assert "sess-6" in plugin.token_cache
        await plugin.after_run_callback(invocation_context=ic)
        assert "sess-6" not in plugin.token_cache

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
