import logging
from contextvars import ContextVar
from typing import Callable

import httpx
from a2a.auth.user import User
from a2a.server.agent_execution import RequestContext, SimpleRequestContextBuilder
from a2a.server.context import ServerCallContext
from a2a.server.tasks import TaskStore
from a2a.types import MessageSendParams, Task
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request

# --- Configure Logging ---
logger = logging.getLogger(__name__)

# Context variable to store user_id for propagation to sub-agents
_current_user_id: ContextVar[str | None] = ContextVar("current_user_id", default=None)

# Constants for user ID handling
USER_ID_HEADER = "X-User-ID"
USER_ID_PREFIX = "A2A_USER_"


# --- Request Utility Functions ---


def extract_user_id(context: RequestContext) -> str:
    """Extract user_id from RequestContext.

    Tries to get user_id from multiple sources in order:
    1. call_context.user.user_name if available (set by KAgentRequestContextBuilder from X-User-ID header)
    2. Context variable (set by middleware from X-User-ID header)
    3. Falls back to a generated ID based on context_id

    Args:
        context: The A2A RequestContext

    Returns:
        The user_id string
    """
    # Get user from call context if available (auth is enabled on a2a server)
    if context.call_context and context.call_context.user and context.call_context.user.user_name:
        return context.call_context.user.user_name

    # Try to get user from context variable (set by middleware)
    user_id_from_context = get_current_user_id()
    if user_id_from_context:
        logger.debug(f"Using user_id from context variable: {user_id_from_context}")
        return user_id_from_context

    # Get user from context id as fallback
    logger.warning(f"No user_id found, falling back to context_id: {context.context_id}")
    return f"{USER_ID_PREFIX}{context.context_id}"


def extract_header(context: ServerCallContext, header_name: str, default: str | None = None) -> str | None:
    """Extract a header value from ServerCallContext.

    Args:
        context: The server call context
        header_name: Name of the header to extract
        default: Default value if header is not found

    Returns:
        The header value or default
    """
    if not context:
        return default

    headers = context.state.get("headers", {})
    return headers.get(header_name, default)


def create_user_from_header(context: ServerCallContext, header_name: str | None = None) -> User | None:
    """Create a KAgentUser from header in ServerCallContext.

    Args:
        context: The server call context
        header_name: Name of the header containing user_id (defaults to X-User-ID)

    Returns:
        KAgentUser instance if user_id found in header, None otherwise
    """
    header_name = header_name or USER_ID_HEADER
    user_id = extract_header(context, header_name)
    if user_id:
        return KAgentUser(user_id=user_id)
    return None


def set_current_user_id(user_id: str | None):
    """Set the current user_id in the context variable for propagation to sub-agents.

    This should be called by the agent executor before running workflow agents
    to ensure user context is propagated through all sub-agent calls.

    Args:
        user_id: The user ID to propagate (or None to clear)
    """
    _current_user_id.set(user_id)


def get_current_user_id() -> str | None:
    """Get the current user_id from the context variable.

    Returns:
        The current user ID or None if not set
    """
    return _current_user_id.get()


async def _inject_user_id_header(request: httpx.Request):
    """Event hook to inject X-User-ID header from context variable.

    This is an async function to be compatible with httpx.AsyncClient event hooks.
    """
    user_id = get_current_user_id()
    if user_id:
        request.headers["X-User-ID"] = user_id


def create_user_propagating_httpx_client(timeout: float = 30.0) -> httpx.AsyncClient:
    """Create an httpx.AsyncClient that propagates user_id via X-User-ID header.

    This client uses a context variable to dynamically inject the X-User-ID header
    at request time, enabling user context propagation through workflow agents.

    Args:
        timeout: Request timeout in seconds

    Returns:
        Configured AsyncClient with user ID propagation
    """
    return httpx.AsyncClient(
        timeout=httpx.Timeout(timeout=timeout),
        event_hooks={"request": [_inject_user_id_header]},
    )


# --- Supporting Classes ---


class KAgentUser(User):
    """A simple user implementation for KAgent integration."""

    def __init__(self, user_id: str):
        self.user_id = user_id

    @property
    def is_authenticated(self) -> bool:
        return False

    @property
    def user_name(self) -> str:
        return self.user_id


class KAgentRequestContextBuilder(SimpleRequestContextBuilder):
    """Request context builder that extracts user information from headers.

    This builder extracts user information from incoming request headers
    and injects it into the request context.
    """

    def __init__(self, task_store: TaskStore):
        super().__init__(task_store=task_store)

    async def build(
        self,
        params: MessageSendParams | None = None,
        task_id: str | None = None,
        context_id: str | None = None,
        task: Task | None = None,
        context: ServerCallContext | None = None,
    ) -> RequestContext:
        if context:
            # Check if user is already set by A2A authentication middleware
            # If not, try to extract from header
            if not context.user:
                user = create_user_from_header(context)
                if user:
                    context.user = user
        request_context = await super().build(params, task_id, context_id, task, context)
        return request_context


class UserIdExtractionMiddleware(BaseHTTPMiddleware):
    """FastAPI middleware that extracts X-User-ID header and sets it in context variable.

    This middleware runs before A2A request processing, extracting the X-User-ID header
    from the incoming HTTP request and setting it in the context variable. This ensures
    that user_id is available throughout the request lifecycle, including for sub-agent calls.

    Usage:
        app = FastAPI()
        app.add_middleware(UserIdExtractionMiddleware)
    """

    async def dispatch(self, request: Request, call_next: Callable):
        """Middleware that extracts and sets user_id from X-User-ID header."""
        # Extract X-User-ID from request headers
        user_id = request.headers.get(USER_ID_HEADER)

        if user_id:
            # Set in context variable for use throughout request processing
            set_current_user_id(user_id)
            logger.debug(f"Extracted user_id from X-User-ID header: {user_id}")
        else:
            logger.debug("No X-User-ID header found in request")

        try:
            # Process the request
            response = await call_next(request)
            return response
        finally:
            # Clear context variable after request completes
            set_current_user_id(None)


def create_user_id_extraction_middleware():
    """Create FastAPI middleware that extracts X-User-ID header and sets it in context variable.

    This function is kept for backward compatibility but is deprecated.
    Use UserIdExtractionMiddleware class directly with app.add_middleware() instead.

    Usage (deprecated):
        app = FastAPI()
        app.middleware("http")(create_user_id_extraction_middleware())

    Usage (recommended):
        app = FastAPI()
        app.add_middleware(UserIdExtractionMiddleware)

    Returns:
        The UserIdExtractionMiddleware class (not an instance)
    """
    return UserIdExtractionMiddleware
