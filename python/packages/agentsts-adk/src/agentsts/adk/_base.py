"""Google ADK-specific STS integration."""

import hashlib
import inspect
import logging
import time
from typing import Awaitable, Callable, Dict, List, Optional, Union

import jwt
from agentsts.core import STSIntegrationBase, TokenType
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

logger = logging.getLogger(__name__)

HEADERS_KEY = "headers"

# Bounds an entry whose token carries no usable expiry. The cache holds one entry
# per (session, subject), so a token without an expiry would otherwise pin an
# entry per caller for the lifetime of the process.
MAX_CACHE_TTL_SECONDS = 300


def _acting_credential(state: dict) -> Optional[str]:
    """Return the credential this caller presented, which the cache is keyed on.

    Deliberately not ``get_subject_token``: that hook receives the whole session
    state, so an implementation reading a session-scoped field would return one
    value for every caller and collapse the key back to a single entry per
    session. Only the inbound Authorization header is caller-scoped by
    construction.
    """
    headers = state.get(HEADERS_KEY, None)
    return _extract_jwt_from_headers(headers)


def _default_get_subject_token(state: dict) -> Optional[str]:
    """Default subject token retrieval from Authorization header in session state."""
    return _acting_credential(state)


def _subject_key(token: Optional[str]) -> str:
    """Derive a stable per-principal cache discriminator from a bearer token.

    Prefers the issuer-scoped ``sub`` claim. ``sub`` is unique only within an
    issuer, so it is paired with ``iss``. Opaque, sub-less and issuer-less tokens
    fall back to a hash of the raw token so they still partition per principal:
    an absent ``iss`` leaves ``sub`` unqualified, which two issuers that both omit
    it could collide on.

    NOTE: this parses the token without verification and uses it only to
    partition the cache. On a cache hit the key selects which cached delegated
    token the caller receives, so authenticating the inbound bearer remains the
    caller's (upstream) responsibility; tokens are validated server-side during
    the STS exchange on a miss.
    """
    if not token:
        return ""
    try:
        claims = jwt.decode(token, options={"verify_signature": False})
    except Exception:
        claims = {}
    sub = claims.get("sub")
    iss = claims.get("iss")
    if sub and iss:
        return f"{iss}\0{sub}"
    return "h:" + hashlib.sha256(token.encode()).hexdigest()


class ADKSTSIntegration(STSIntegrationBase):
    """Google ADK-specific STS integration.

    By default, the subject token is read from the ``Authorization`` header
    stored in the session state under the ``headers`` key.  To retrieve the
    subject token from a custom source, pass a ``get_subject_token`` callback::

        integration = ADKSTSIntegration(
            well_known_uri="https://example.com/.well-known/sts",
            get_subject_token=lambda state: state.get("my_custom_token_key"),
        )

    The callback receives ``session.state`` (a dict) and should return the
    subject token string, or ``None`` if not available.
    """

    def __init__(
        self,
        well_known_uri: str,
        service_account_token_path: Optional[str] = None,
        fetch_actor_token: Optional[Union[Callable[[], str], Callable[[], Awaitable[str]]]] = None,
        timeout: int = 5,
        verify_ssl: bool = True,
        use_issuer_host: bool = False,
        get_subject_token: Optional[Callable[[dict], Optional[str]]] = None,
    ):
        """Initialize the ADK STS integration.

        Args:
            well_known_uri: Well-known configuration URI for the STS server
            service_account_token_path: Path to service account token file (ignored if fetch_actor_token is set)
            fetch_actor_token: Optional callable (sync or async) that returns an actor token
            timeout: Request timeout in seconds
            verify_ssl: Whether to verify SSL certificates
            use_issuer_host: Replace the host:port in token_endpoint with the host:port from well_known_uri
            get_subject_token: Optional callback that takes session.state (dict) and returns
                the subject token string or None. If not set, defaults to extracting the
                JWT from the Authorization header in session.state["headers"].
        """
        super().__init__(
            well_known_uri=well_known_uri,
            service_account_token_path=service_account_token_path,
            fetch_actor_token=fetch_actor_token,
            timeout=timeout,
            verify_ssl=verify_ssl,
            use_issuer_host=use_issuer_host,
            get_subject_token=get_subject_token or _default_get_subject_token,
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

    def __init__(
        self,
        sts_integration: Optional[STSIntegrationBase] = None,
        resource: Optional[Union[str, List[str]]] = None,
        audience: Optional[Union[str, List[str]]] = None,
    ):
        """Initialize the token propagation plugin.

        Args:
            sts_integration: The ADK STS integration instance
            resource: RFC 8707 resource indicator sent on the token exchange to
                scope the issued token to a target backend. Omitted when None.
            audience: RFC 8693 audience sent on the token exchange. Omitted when None.
        """
        super().__init__("ADKTokenPropagationPlugin")
        self.sts_integration = sts_integration
        self.resource = resource
        self.audience = audience
        self.token_cache: Dict[str, _TokenCacheEntry] = {}
        self.actor_token_cache: Optional[_TokenCacheEntry] = None
        # Earliest expiry across token_cache; None when no cached token expires.
        self._earliest_expiry: Optional[int] = None

    def add_to_agent(self, agent: BaseAgent):
        """
        Add the plugin to an ADK LLM agent by updating its MCP toolset
        Call this once when setting up the agent; do not call it at runtime.
        """
        agent_name = getattr(agent, "name", "unknown")
        logger.debug(f"add_to_agent called for agent {agent_name}")

        if not isinstance(agent, LlmAgent):
            logger.debug(f"add_to_agent: agent {agent_name} is not LlmAgent, skipping")
            return

        if not agent.tools:
            logger.debug(f"add_to_agent: agent {agent_name} has no tools, skipping")
            return

        for tool in agent.tools:
            if isinstance(tool, McpToolset):
                mcp_toolset = tool
                mcp_toolset._header_provider = self.header_provider
                logger.debug(f"add_to_agent: updated MCP tool's header provider for agent {agent_name}")

    def header_provider(self, readonly_context: Optional[ReadonlyContext]) -> Dict[str, str]:
        # Runs on every tool call, so it fails closed rather than raising into the
        # invocation: without a context there is no acting subject to key on.
        invocation_context = getattr(readonly_context, "_invocation_context", None)
        if invocation_context is None:
            logger.debug("no invocation context for tool call, leaving existing headers in place")
            return {}

        cache_key = self.cache_key(invocation_context)
        cache_entry = self.token_cache.get(cache_key) if cache_key else None
        if not cache_entry:
            return {}

        logger.debug("Using cached access token for tool invocation")
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
        # Resolve the acting caller before the cache lookup: the cache is keyed by
        # subject, and a session carrying messages from several subjects would
        # otherwise reuse whichever caller arrived first.
        cache_key = self._cache_key_for(
            invocation_context.session.id,
            self._key_input(invocation_context.session.state),
        )
        if cache_key is None:
            logger.debug("subject token not found in session state for token propagation")
            return None

        # Check if we have a valid cached subject token
        cached_entry = self.token_cache.get(cache_key)
        if cached_entry and not _has_token_expired(cached_entry.expiry):
            if cached_entry.expiry:
                current_time = int(time.time())
                logger.debug(f"Using cached subject token (expires in {cached_entry.expiry - current_time}s)")
            else:
                logger.debug("Using cached subject token (no expiry)")
            return None

        # The exchange payload comes from get_subject_token, which the cache key
        # deliberately does not use: see _acting_credential.
        subject_token = self._read_subject_token(invocation_context.session.state)
        if not subject_token:
            logger.debug("subject token not found in session state for token propagation")
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
                    resource=self.resource,
                    audience=self.audience,
                )
            except Exception as e:
                logger.warning(f"STS token exchange failed: {e}")
                return None

        # Extract expiry from the token, bounding tokens that carry none so every
        # entry stays evictable.
        expiry = _extract_jwt_expiry(subject_token)
        if expiry is None:
            expiry = int(time.time()) + MAX_CACHE_TTL_SECONDS

        # Cache the token with metadata
        self.token_cache[cache_key] = _TokenCacheEntry(
            token=subject_token,
            expiry=expiry,
        )
        self._earliest_expiry = _earlier_expiry(self._earliest_expiry, expiry)
        logger.debug("Cached new subject token")
        return None

    def _read_subject_token(self, state: dict) -> Optional[str]:
        """Resolve the acting caller's subject token from session state.

        get_subject_token is caller-supplied, so a raising implementation fails
        closed (no token propagated) instead of aborting the agent run.
        """
        get_subject_token = (
            self.sts_integration.get_subject_token
            if self.sts_integration and self.sts_integration.get_subject_token
            else _default_get_subject_token
        )
        try:
            return get_subject_token(state)
        except Exception as e:
            logger.warning(f"Failed to read subject token from session state: {e}")
            return None

    def _key_input(self, state: dict) -> Optional[str]:
        """Return the value the cache key is derived from.

        The caller's own credential wins when there is one, so a session carrying
        several callers partitions per caller even if get_subject_token reads a
        session-scoped field and would return one value for all of them.

        Without an inbound credential there is nothing caller-scoped to key on,
        so the hook's output is used: that mode has no per-caller identity to
        preserve, and one entry per session is the correct partitioning for it.
        """
        return _acting_credential(state) or self._read_subject_token(state)

    def _cache_key_for(self, session_id: str, subject_token: Optional[str]) -> Optional[str]:
        subject = _subject_key(subject_token)
        if not subject:
            # An empty subject identifies no principal, so it yields no key: an
            # entry stored under it would be shared by every credential-less
            # caller in the session.
            return None
        return f"{session_id}\0{subject}"

    def cache_key(self, invocation_context: InvocationContext) -> Optional[str]:
        """Key the cache on the session and the acting subject, so a session
        carrying messages from several subjects keeps one token per subject
        instead of collapsing onto whichever arrived first. None when the caller
        presents no credential to derive a subject from."""
        session = invocation_context.session
        return self._cache_key_for(session.id, self._key_input(session.state))

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
        self._sweep_expired_subject_tokens()

        # Clean up expired actor token cache
        if self.actor_token_cache and _has_token_expired(self.actor_token_cache.expiry):
            logger.debug("Removing expired actor token from cache")
            self.actor_token_cache = None

        return None

    def _sweep_expired_subject_tokens(self) -> None:
        """Drop every expired subject token from the cache.

        A session holds one entry per subject and only the acting subject's key
        is derivable here, so entries belonging to other subjects and other
        sessions are swept too; scoping the sweep to the current session would
        keep the entries of sessions that never run again forever. The earliest
        expiry gates the scan, so a growing cache is only walked when there is
        something to evict.
        """
        if self._earliest_expiry is None or not _has_token_expired(self._earliest_expiry):
            return

        earliest_expiry: Optional[int] = None
        for key, entry in list(self.token_cache.items()):
            if _has_token_expired(entry.expiry):
                logger.debug("Removing expired subject token from cache")
                self.token_cache.pop(key, None)
                continue
            earliest_expiry = _earlier_expiry(earliest_expiry, entry.expiry)
        self._earliest_expiry = earliest_expiry


def _earlier_expiry(current: Optional[int], candidate: Optional[int]) -> Optional[int]:
    """Return the earlier of two expiries; None means "does not expire"."""
    if candidate is None:
        return current
    if current is None:
        return candidate
    return min(current, candidate)


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
