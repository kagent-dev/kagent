"""Context variable module for passing user_id through async call chains."""

from contextvars import ContextVar
from typing import Optional

_user_id_context: ContextVar[Optional[str]] = ContextVar("user_id", default=None)


def set_user_id(user_id: str) -> None:
    """Set the user_id in the current async context.

    Args:
        user_id: The user identifier to store in context.
    """
    _user_id_context.set(user_id)


def get_user_id() -> Optional[str]:
    """Get the user_id from the current async context.

    Returns:
        The user_id if set, None otherwise.
    """
    return _user_id_context.get()


def clear_user_id() -> None:
    """Clear the user_id from the current async context."""
    _user_id_context.set(None)
