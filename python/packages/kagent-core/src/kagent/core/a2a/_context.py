from contextvars import ContextVar

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
