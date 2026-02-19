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
    """Plugin for propagating STS tokens to ADK tools."""

    def __init__(self, sts_integration: Optional[STSIntegrationBase] = None):
        """Initialize the token propagation plugin.

        Args:
            sts_integration: The ADK STS integration instance
        """
        super().__init__("ADKTokenPropagationPlugin")
        self.sts_integration = sts_integration
        self.token_cache: Dict[str, str] = {}

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
        # access save token
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
        """Propagate token to model before execution."""
        headers = invocation_context.session.state.get(HEADERS_KEY, None)
        subject_token = _extract_jwt_from_headers(headers)
        if not subject_token:
            logger.debug("No subject token found in headers for token propagation")
            return None
        if self.sts_integration:
            try:
                subject_token = await self.sts_integration.exchange_token(
                    subject_token=subject_token,
                    subject_token_type=TokenType.JWT,
                    actor_token=self.sts_integration._actor_token,
                    actor_token_type=TokenType.JWT if self.sts_integration._actor_token else None,
                )
            except Exception as e:
                logger.warning(f"STS token exchange failed: {e}")
                return None
        # no sts, just propagate the subject token upstream
        self.token_cache[self.cache_key(invocation_context)] = subject_token
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
        # delete token after run
        self.token_cache.pop(self.cache_key(invocation_context), None)
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
