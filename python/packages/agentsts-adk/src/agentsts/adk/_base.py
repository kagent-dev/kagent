"""Google ADK-specific STS integration."""

import inspect
import logging
import time
from typing import Any, Awaitable, Callable, Dict, Optional, Union

import jwt
from google.adk.agents import BaseAgent, LlmAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.auth.auth_credential import (
    AuthCredential,
    AuthCredentialTypes,
    HttpAuth,
    HttpCredentials,
)
from google.adk.events.event import Event
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import BaseSessionService
from google.adk.sessions.session import Session
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.mcp_tool.mcp_toolset import MCPToolset, McpToolset
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
        fetch_actor_token: Optional[Union[Callable[[], str], Callable[[], Awaitable[str]]]] = None,
        timeout: int = 5,
        verify_ssl: bool = True,
        use_issuer_host: bool = False,
        additional_config: Optional[Dict[str, Any]] = None,
    ):
        """Initialize the ADK STS integration.

        Args:
            well_known_uri: Well-known configuration URI for the STS server
            service_account_token_path: Path to service account token file (ignored if fetch_actor_token is set)
            fetch_actor_token: Optional callable (sync or async) that returns an actor token
            timeout: Request timeout in seconds
            verify_ssl: Whether to verify SSL certificates
            use_issuer_host: Replace the host:port in token_endpoint with the host:port from well_known_uri
            additional_config: Additional configuration
        """
        super().__init__(
            well_known_uri=well_known_uri,
            service_account_token_path=service_account_token_path,
            fetch_actor_token=fetch_actor_token,
            timeout=timeout,
            verify_ssl=verify_ssl,
            use_issuer_host=use_issuer_host,
            additional_config=additional_config,
        )


class _TokenCacheEntry:
    """Cache entry for access tokens with metadata."""

    def __init__(self, token: str, expiry: Optional[int] = None):
        """Initialize token cache entry.

        Args:
            token: The access token
            expiry: Token expiry timestamp (Unix epoch), if available
        """
        self.token = token
        self.expiry = expiry


class ADKTokenPropagationPlugin(BasePlugin):
    """Plugin for propagating STS tokens to ADK tools."""

    def __init__(self, sts_integration: Optional[STSIntegrationBase] = None):
        """Initialize the token propagation plugin.

        Args:
            sts_integration: The ADK STS integration instance
        """
        super().__init__("ADKTokenPropagationPlugin")
        self.sts_integration = sts_integration
        self.token_cache: Dict[str, _TokenCacheEntry] = {}
        self.actor_token_cache: Optional[_TokenCacheEntry] = None

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
            if isinstance(tool, McpToolset):
                mcp_toolset = tool
                mcp_toolset._header_provider = self.header_provider
                logger.debug("Updated tool connection params to include access token from STS server")

    def header_provider(self, readonly_context: Optional[ReadonlyContext]) -> Dict[str, str]:
        # access saved token
        cache_entry = self.token_cache.get(self.cache_key(readonly_context._invocation_context))
        if not cache_entry:
            return {}

        return {
            "Authorization": f"Bearer {cache_entry.token}",
        }

    @override
    async def before_run_callback(
        self,
        *,
        invocation_context: InvocationContext,
    ) -> Optional[dict]:
        """Propagate token to model before execution."""
        cache_key = self.cache_key(invocation_context)

        # Check if we have a valid cached subject token
        cached_entry = self.token_cache.get(cache_key)
        if cached_entry and not _has_token_expired(cached_entry.expiry):
            if cached_entry.expiry:
                current_time = int(time.time())
                logger.debug(f"Using cached subject token (expires in {cached_entry.expiry - current_time}s)")
            else:
                logger.debug("Using cached subject token (no expiry)")
            return None

        # No valid cached token, need to get/exchange subject token
        headers = invocation_context.session.state.get(HEADERS_KEY, None)
        subject_token = _extract_jwt_from_headers(headers)
        if not subject_token:
            logger.debug("No subject token found in headers for token propagation")
            return None

        if self.sts_integration:
            # Get actor token (from cache or fetch dynamically)
            actor_token = await self._get_actor_token()
            if actor_token is None and self.sts_integration.fetch_actor_token:
                # Dynamic fetch failed; already logged a warning in _get_actor_token
                return None

            try:
                subject_token = await self.sts_integration.exchange_token(
                    subject_token=subject_token,
                    subject_token_type=TokenType.JWT,
                    actor_token=actor_token,
                    actor_token_type=TokenType.JWT if actor_token else None,
                )
            except Exception as e:
                logger.warning(f"STS token exchange failed: {e}")
                return None

        # Extract expiry from the token
        expiry = _extract_jwt_expiry(subject_token)

        # Cache the token with metadata
        self.token_cache[cache_key] = _TokenCacheEntry(
            token=subject_token,
            expiry=expiry,
        )
        logger.debug("Cached new subject token")
        return None

    def cache_key(self, invocation_context: InvocationContext) -> str:
        """Generate a cache key based on the session ID."""
        return invocation_context.session.id

    async def _get_actor_token(self) -> Optional[str]:
        """Get actor token from cache or fetch dynamically.

        Returns:
            Actor token string if available, None otherwise
        """
        if not self.sts_integration:
            return None

        # Use static token if no dynamic fetch function
        if not self.sts_integration.fetch_actor_token:
            return self.sts_integration._actor_token

        # Check cache for unexpired dynamic token
        if self.actor_token_cache:
            if not _has_token_expired(self.actor_token_cache.expiry):
                # Token is still valid
                if self.actor_token_cache.expiry:
                    current_time = int(time.time())
                    logger.debug(
                        f"Using cached actor token (expires in {self.actor_token_cache.expiry - current_time}s)"
                    )
                else:
                    logger.debug("Using cached actor token (no expiry)")
                return self.actor_token_cache.token
            else:
                logger.debug("Cached actor token expired, fetching new one")

        # Fetch new actor token
        try:
            if inspect.iscoroutinefunction(self.sts_integration.fetch_actor_token):
                actor_token = await self.sts_integration.fetch_actor_token()
            else:
                actor_token = self.sts_integration.fetch_actor_token()

            # Extract expiry and cache the token
            expiry = _extract_jwt_expiry(actor_token)
            self.actor_token_cache = _TokenCacheEntry(token=actor_token, expiry=expiry)
            logger.debug("Fetched and cached new actor token")
            return actor_token

        except Exception as e:
            logger.warning(f"Failed to fetch actor token dynamically: {e}")
            return None

    @override
    async def after_run_callback(
        self,
        *,
        invocation_context: InvocationContext,
    ) -> Optional[dict]:
        """Clean up expired tokens after run, preserving valid tokens."""
        cache_key = self.cache_key(invocation_context)
        cache_entry = self.token_cache.get(cache_key)

        # Clean up subject token cache - only remove if expired
        if cache_entry and _has_token_expired(cache_entry.expiry):
            logger.debug("Removing expired subject token from cache")
            self.token_cache.pop(cache_key, None)

        # Clean up expired actor token cache
        if self.actor_token_cache and _has_token_expired(self.actor_token_cache.expiry):
            logger.debug("Removing expired actor token from cache")
            self.actor_token_cache = None

        return None


def _has_token_expired(expiry: Optional[int], buffer_seconds: int = 5) -> bool:
    """Check if a token has expired or will expire soon.

    Args:
        expiry: Token expiry timestamp (Unix epoch), or None if no expiry
        buffer_seconds: Additional buffer time in seconds to treat tokens
                       expiring soon as already expired (default: 5)

    Returns:
        True if token has expired or will expire within buffer_seconds,
        False if still valid or no expiry
    """
    if expiry is None:
        return False  # No expiry means never expires

    current_time = int(time.time())
    return expiry <= (current_time + buffer_seconds)


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


def _extract_jwt_expiry(token: str) -> Optional[int]:
    """Extract expiry timestamp from JWT token.

    NOTE: This function does NOT validate the token signature.
    It is only used for cache management, not security decisions.
    Token validation happens in the STS server during exchange.

    Args:
        token: JWT token string

    Returns:
        Expiry timestamp (Unix epoch) if found, None otherwise
    """
    try:
        # Decode without verification (we only need the expiry claim)
        decoded = jwt.decode(token, options={"verify_signature": False})
        expiry = decoded.get("exp")
        if expiry:
            logger.debug(f"Extracted JWT expiry: {expiry}")
            return int(expiry)

        logger.debug("No 'exp' claim found in JWT")
        return None
    except Exception as e:
        logger.warning(f"Failed to extract JWT expiry: {e}")
        return None
