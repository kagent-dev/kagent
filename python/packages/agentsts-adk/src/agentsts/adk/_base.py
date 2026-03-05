"""Google ADK-specific STS integration."""

import logging
from typing import Any, Dict, Optional

from google.adk.agents import BaseAgent, LlmAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.auth.auth_credential import AuthCredential, AuthCredentialTypes, HttpAuth, HttpCredentials
from google.adk.events.event import Event
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import BaseSessionService
from google.adk.sessions.session import Session
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.mcp_tool.mcp_tool import McpTool
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

    Token exchange lifecycle:
    1. before_run_callback: extracts the subject token from request headers
       and stores it for the duration of the invocation.
    2. before_tool_callback: when an MCP tool is about to be called and STS
       is configured, exchanges the subject token (async) and caches the
       access token so header_provider can read it.
    3. header_provider (sync): returns cached access token as Authorization
       header -- called by McpToolset/McpTool during MCP session setup.
    4. after_run_callback: cleans up all cached state.
    """

    def __init__(self, sts_integration: Optional[STSIntegrationBase] = None):
        """Initialize the token propagation plugin.

        Args:
            sts_integration: The ADK STS integration instance
        """
        super().__init__("ADKTokenPropagationPlugin")
        self.sts_integration = sts_integration
        self.token_cache: Dict[str, str] = {}
        self._subject_tokens: Dict[str, str] = {}

    def add_to_agent(self, agent: BaseAgent):
        """
        Add the plugin to an ADK LLM agent by updating its MCP toolset
        Call this once when setting up the agent; do not call it at runtime.
        """
        if not isinstance(agent, LlmAgent):
            return

        if not agent.tools:
            return

        for tool in agent.tools:
            if isinstance(tool, MCPToolset):
                mcp_toolset = tool
                mcp_toolset._header_provider = self.header_provider
                logger.debug("Updated tool connection params to include access token from STS server")

    def header_provider(self, readonly_context: Optional[ReadonlyContext]) -> Dict[str, str]:
        access_token = self.token_cache.get(self.cache_key(readonly_context._invocation_context), "")
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
        """Extract and store the subject token for later exchange."""
        headers = invocation_context.session.state.get(HEADERS_KEY, None)
        subject_token = _extract_jwt_from_headers(headers)
        if not subject_token:
            logger.debug("No subject token found in headers for token propagation")
            return None
        key = self.cache_key(invocation_context)
        self._subject_tokens[key] = subject_token
        if not self.sts_integration:
            # No STS -- propagate the subject token directly so
            # header_provider can return it on the first tool call.
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
        """Exchange the subject token via STS before each MCP tool call."""
        if not self.sts_integration:
            return None
        # Only exchange tokens for MCP tool calls. Other tool types
        # (memory tools, AgentTool, etc.) don't use header_provider and
        # have their own auth mechanisms, so exchanging here would be a
        # wasted HTTP round-trip to the STS.
        if not isinstance(tool, McpTool):
            return None

        key = self.cache_key(tool_context._invocation_context)
        # Already exchanged for this session
        if key in self.token_cache:
            return None

        subject_token = self._subject_tokens.get(key)
        if not subject_token:
            return None

        try:
            access_token = await self.sts_integration.exchange_token(
                subject_token=subject_token,
                subject_token_type=TokenType.JWT,
                actor_token=self.sts_integration._actor_token,
                actor_token_type=TokenType.JWT if self.sts_integration._actor_token else None,
            )
            self.token_cache[key] = access_token
        except Exception as e:
            logger.warning(f"STS token exchange failed: {e}")

        return None

    def cache_key(self, invocation_context: InvocationContext) -> str:
        """Generate a cache key based on the session ID."""
        return invocation_context.session.id

    @override
    async def after_run_callback(
        self,
        *,
        invocation_context: InvocationContext,
    ) -> Optional[dict]:
        key = self.cache_key(invocation_context)
        self.token_cache.pop(key, None)
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
