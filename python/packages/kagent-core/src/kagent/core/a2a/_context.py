from collections.abc import Iterator
from contextlib import contextmanager
from contextvars import ContextVar

from a2a.server.context import ServerCallContext

_current_user_id: ContextVar[str | None] = ContextVar("kagent_user_id", default=None)


def set_request_user_id(user_id: str | None) -> None:
    """Store the caller's user ID for the current async context.

    Must be called before any outgoing HTTP requests to the kagent controller
    so that the token service event hook can inject X-User-Id.
    """
    _current_user_id.set(user_id)


def get_request_user_id() -> str | None:
    """Return the caller's user ID for the current async context."""
    return _current_user_id.get()


def get_call_context_user_id(context: ServerCallContext | None) -> str | None:
    """Return the effective user forwarded in an A2A server call context."""
    if context is None:
        return None
    headers = context.state.get("headers", {})
    user_id = headers.get("x-user-id")
    return user_id if isinstance(user_id, str) and user_id else None


@contextmanager
def scoped_request_user_id(user_id: str | None) -> Iterator[None]:
    """Temporarily expose a scoped user to controller HTTP request hooks."""
    token = _current_user_id.set(user_id)
    try:
        yield
    finally:
        _current_user_id.reset(token)
