"""Google ADK-specific STS integration."""

import logging
from typing import Any, Dict, Optional

from agw_status_client import TokenType
from google.adk.auth.auth_credential import AuthCredential, AuthCredentialTypes, HttpAuth, HttpCredentials
from google.adk.events.event import Event
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.adk.sessions.base_session_service import BaseSessionService
from google.adk.sessions.session import Session
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.tool_context import ToolContext
from typing_extensions import override

from agw.base import STSIntegrationBase

logger = logging.getLogger(__name__)

ACCESS_TOKEN_KEY = "access_token"
SUBJECT_TOKEN_KEY = "subject_token"


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

    def create_auth_credential(self, access_token: str) -> AuthCredential:
        return create_adk_auth_credential(access_token)

    async def get_auth_credential(
        self,
        subject_token: str,
        subject_token_type: TokenType = TokenType.JWT,
    ) -> AuthCredential:
        access_token = await self.exchange_token(subject_token, subject_token_type)
        return self.create_auth_credential(access_token)


class ADKTokenPropagationPlugin(BasePlugin):
    """Plugin for propagating STS tokens to ADK tools."""

    def __init__(self, sts_integration: Optional[ADKSTSIntegration] = None):
        """Initialize the token propagation plugin.

        Args:
            sts_integration: The ADK STS integration instance
        """
        super().__init__("ADKTokenPropagationPlugin")
        self.sts_integration = sts_integration

    @override
    async def before_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Optional[dict]:
        """Propagate token to MCP tools before execution.

        Args:
            tool: The tool being executed
            tool_args: Arguments for the tool
            tool_context: Context for the tool execution

        Returns:
            Modified tool arguments with credential if applicable
        """
        if isinstance(tool, MCPTool):
            logger.debug("Setting up token propagation for ADK tool: %s", tool.name)

            # get subject's access token from session state or access token from session state
            subject_token = tool_context._invocation_context.session.state.get(SUBJECT_TOKEN_KEY, None)
            access_token = tool_context._invocation_context.session.state.get(ACCESS_TOKEN_KEY, None)
            if subject_token and self.sts_integration is not None:
                try:
                    credential = await self.sts_integration.get_auth_credential(subject_token=subject_token)
                except Exception as e:
                    logger.error(f"Token exchange failed for tool {tool.name}: {e}")
                    logger.warning("Continuing without STS token propagation due to token exchange failure")
                    return None
            elif access_token:
                try:
                    credential = create_adk_auth_credential(access_token=access_token)
                except Exception as e:
                    logger.error(f"Token exchange failed for tool {tool.name}: {e}")
                    logger.warning("Continuing without STS token propagation due to token exchange failure")
                    return None
            else:
                logger.warning("No access token available for ADK tool: %s", tool.name)
                return None
            if credential:
                logger.debug("Propagating STS token in ADK tool call: %s", tool.name)
                return await tool._run_async_impl(args=tool_args, tool_context=tool_context, credential=credential)
            else:
                logger.warning("No STS token available for ADK tool: %s", tool.name)

        return None


class ADKSessionService(InMemorySessionService):
    """Wrapper for the ADK session service that includes STS token in session state."""

    def __init__(self, sts_integration: ADKSTSIntegration, wrapped_service: Optional[BaseSessionService] = None):
        """Initialize the session service wrapper.

        Args:
            sts_integration: The ADK STS integration instance
            wrapped_service: Optional session service to wrap (any BaseSessionService implementation).
                           If None, uses InMemorySessionService
        """
        super().__init__()
        self._wrapped_service = wrapped_service
        self.sts_integration = sts_integration
        self._subject_token = None
        self._access_token = None

    @override
    async def get_session(self, *, app_name: str, user_id: str, session_id: str, config=None):
        """Get session with STS token in state."""
        if self._wrapped_service is not None:
            session = await self._wrapped_service.get_session(
                app_name=app_name,
                user_id=user_id,
                session_id=session_id,
                config=config,
            )
        else:
            session = await super().get_session(
                app_name=app_name,
                user_id=user_id,
                session_id=session_id,
                config=config,
            )

        if session is not None:
            if session.state is None:
                session.state = {}

            # Add the subject token to session state
            if self._subject_token:
                session.state[SUBJECT_TOKEN_KEY] = self._subject_token
            elif self._access_token:
                session.state[ACCESS_TOKEN_KEY] = self._access_token

        return session

    @override
    async def create_session(self, *, app_name: str, user_id: str, state=None, session_id=None):
        """Create session with STS token in state."""
        if self._wrapped_service is not None:
            session = await self._wrapped_service.create_session(
                app_name=app_name,
                user_id=user_id,
                state=state,
                session_id=session_id,
            )
        else:
            session = await super().create_session(
                app_name=app_name,
                user_id=user_id,
                state=state,
                session_id=session_id,
            )

        if session.state is None:
            session.state = {}

        if self._subject_token:
            session.state[SUBJECT_TOKEN_KEY] = self._subject_token
        elif self._access_token:
            session.state[ACCESS_TOKEN_KEY] = self._access_token

        return session

    @override
    async def delete_session(self, *, app_name: str, user_id: str, session_id: str):
        """Delete session."""
        if self._wrapped_service is not None:
            return await self._wrapped_service.delete_session(
                app_name=app_name,
                user_id=user_id,
                session_id=session_id,
            )
        else:
            return await super().delete_session(
                app_name=app_name,
                user_id=user_id,
                session_id=session_id,
            )

    @override
    async def list_sessions(self, *, app_name: str, user_id: str):
        """List sessions."""
        if self._wrapped_service is not None:
            return await self._wrapped_service.list_sessions(
                app_name=app_name,
                user_id=user_id,
            )
        else:
            return await super().list_sessions(
                app_name=app_name,
                user_id=user_id,
            )

    @override
    async def append_event(self, session: Session, event: Event):
        """Append event to session."""
        if self._wrapped_service is not None:
            return await self._wrapped_service.append_event(session=session, event=event)
        else:
            return await super().append_event(session=session, event=event)

    def _store_subject_token(self, subject_token: str):
        self._subject_token = subject_token

    def _store_access_token(self, access_token: str):
        self._access_token = access_token


class ADKRunner(Runner):
    """Custom runner for ADK access token passthrough"""

    def __init__(self, session_service: ADKSessionService, access_token_passthrough: bool = False, **kwargs):
        super().__init__(session_service=session_service, **kwargs)
        self.access_token_passthrough = access_token_passthrough

    @override
    async def run_async(self, *args, **kwargs):
        headers = kwargs.pop("headers", {})
        user_jwt = extract_jwt_from_headers(headers)
        if user_jwt:
            if self.access_token_passthrough:
                self.session_service._store_access_token(user_jwt)
            else:
                self.session_service._store_subject_token(user_jwt)
        else:
            logger.debug("No JWT found in headers")
        async for event in super().run_async(*args, **kwargs):
            yield event


def create_adk_auth_credential(access_token: str) -> AuthCredential:
    credential = AuthCredential(
        auth_type=AuthCredentialTypes.HTTP,
        http=HttpAuth(
            scheme="bearer",
            credentials=HttpCredentials(token=access_token),
        ),
    )

    logger.debug("Successfully configured ADK with access token")
    return credential


def extract_jwt_from_headers(headers: dict[str, str]) -> Optional[str]:
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
