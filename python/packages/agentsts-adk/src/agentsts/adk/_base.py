"""Google ADK-specific STS integration."""

import logging
from typing import Any, Dict, Optional

from google.adk.agents.invocation_context import InvocationContext
from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.mcp_tool.mcp_toolset import MCPToolset
from google.adk.tools.tool_context import ToolContext
from typing_extensions import override

from agentsts.core import STSIntegrationBase, TokenType

logger = logging.getLogger(__name__)

HEADERS_KEY = "headers"


class ADKSTSIntegration(STSIntegrationBase):
    """Google ADK-specific STS integration."""

    def __init__(
        self,
        well_known_uri: str,
        service_account_token_path: Optional[str] = None,
        timeout: int = 5,
        verify_ssl: bool = True,
        additional_config: Optional[Dict[str, Any]] = None,
    ):
        super().__init__(well_known_uri, service_account_token_path, timeout, verify_ssl, additional_config)


class ADKTokenPropagationPlugin(BasePlugin):
    """Plugin for propagating STS tokens to ADK tools.

    Supports audience-scoped token exchange: each MCP toolset can declare
    an STS audience so the exchanged token is scoped to that specific service.
    """

    def __init__(self, sts_integration: Optional[STSIntegrationBase] = None):
        """Initialize the token propagation plugin.

        Args:
            sts_integration: The ADK STS integration instance
        """
        super().__init__("ADKTokenPropagationPlugin")
        self.sts_integration = sts_integration
        self.token_cache: Dict[str, str] = {}  # cache_key -> access_token
        self._subject_tokens: Dict[str, str] = {}  # session_id -> subject_token
        self._audience_map: Dict[int, str] = {}  # id(session_manager) -> audience

    def register_toolset(self, toolset: MCPToolset, audience: Optional[str]) -> None:
        """Register a toolset's session manager -> audience mapping.

        Called during agent setup (to_agent) so the before_tool_callback
        can resolve the audience for each MCP tool at runtime.
        """
        if audience and hasattr(toolset, "_mcp_session_manager"):
            self._audience_map[id(toolset._mcp_session_manager)] = audience

    def header_provider(
        self, readonly_context: Optional[ReadonlyContext], audience: Optional[str] = None
    ) -> Dict[str, str]:
        """Return cached access token as an Authorization header.

        Args:
            readonly_context: ADK readonly context with invocation info.
            audience: Optional audience to look up the correct scoped token.
        """
        key = self.cache_key(readonly_context._invocation_context, audience)
        access_token = self.token_cache.get(key, "")
        if not access_token:
            return {}

        return {
            "Authorization": f"Bearer {access_token}",
        }

    @override
    async def before_run_callback(
        self,
        *,
        invocation_context: InvocationContext,
    ) -> Optional[dict]:
        """Extract and store the subject token from request headers.

        Token exchange is deferred to before_tool_callback so each tool
        can exchange with its own audience.
        """
        headers = invocation_context.session.state.get(HEADERS_KEY, None)
        subject_token = _extract_jwt_from_headers(headers)
        if not subject_token:
            logger.debug("No subject token found in headers for token propagation")
            return None

        key = self.cache_key(invocation_context)
        self._subject_tokens[key] = subject_token

        # When there is no STS integration, propagate the subject token
        # directly under the empty-audience cache key (backward compat).
        if not self.sts_integration:
            self.token_cache[key] = subject_token

        return None

    @override
    async def before_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Optional[dict]:
        """Exchange token with audience scoping on first MCP tool call.

        Only fires for MCPTool instances. Looks up the audience from the
        tool's session manager and exchanges (or reuses) a scoped token.
        """
        if not self.sts_integration:
            return None
        if not isinstance(tool, MCPTool):
            return None

        invocation_context = tool_context._invocation_context
        audience = self._resolve_audience(tool)
        key = self.cache_key(invocation_context, audience)

        if key in self.token_cache:
            return None  # already exchanged for this (session, audience)

        subject_token = self._subject_tokens.get(self.cache_key(invocation_context))
        if not subject_token:
            return None

        try:
            access_token = await self.sts_integration.exchange_token(
                subject_token=subject_token,
                subject_token_type=TokenType.JWT,
                actor_token=self.sts_integration._actor_token,
                actor_token_type=TokenType.JWT if self.sts_integration._actor_token else None,
                audience=audience if audience else None,
            )
            self.token_cache[key] = access_token
        except Exception as e:
            logger.warning("STS token exchange failed for audience '%s': %s", audience, e)

        return None

    def _resolve_audience(self, tool: BaseTool) -> str:
        """Look up audience from the tool's session manager."""
        if hasattr(tool, "_mcp_session_manager"):
            return self._audience_map.get(id(tool._mcp_session_manager), "")
        return ""

    def cache_key(self, invocation_context: InvocationContext, audience: Optional[str] = None) -> str:
        """Generate a cache key based on session ID and audience."""
        session_id = invocation_context.session.id
        if audience:
            return f"{session_id}:{audience}"
        return session_id

    @override
    async def after_run_callback(
        self,
        *,
        invocation_context: InvocationContext,
    ) -> Optional[dict]:
        """Clean up all cached tokens for the completed session."""
        key = self.cache_key(invocation_context)
        keys_to_remove = [
            k for k in self.token_cache if k == key or k.startswith(f"{key}:")
        ]
        for k in keys_to_remove:
            self.token_cache.pop(k, None)
        self._subject_tokens.pop(key, None)
        return None


def _extract_jwt_from_headers(headers: dict[str, str]) -> Optional[str]:
    """Extract JWT from request headers for STS token exchange.

    Args:
        headers: Dictionary of request headers

    Returns:
        JWT token string if found in Authorization header, None otherwise
    """
    if not headers:
        logger.warning("No headers provided for JWT extraction")
        return None

    auth_header = headers.get("Authorization") or headers.get("authorization")
    if not auth_header:
        logger.warning("No Authorization header found in request")
        return None

    if not auth_header.startswith("Bearer "):
        logger.warning("Authorization header must start with Bearer")
        return None

    jwt_token = auth_header.removeprefix("Bearer ").strip()
    if not jwt_token:
        logger.warning("Empty JWT token found in Authorization header")
        return None

    logger.debug(f"Successfully extracted JWT token (length: {len(jwt_token)})")
    return jwt_token
