from contextvars import ContextVar

_current_user_id: ContextVar[str | None] = ContextVar("kagent_user_id", default=None)
# The groups of one user, as the controller encoded them (X-User-Groups), so
# that they are never sent with another user's ID.
_current_user_groups: ContextVar[tuple[str, str] | None] = ContextVar("kagent_user_groups", default=None)


def set_request_user_id(user_id: str | None) -> None:
    """Store the caller's user ID for the current async context.

    Must be called before any outgoing HTTP requests to the kagent controller
    so that the token service event hook can inject X-User-Id.
    """
    _current_user_id.set(user_id)


def get_request_user_id() -> str | None:
    """Return the caller's user ID for the current async context."""
    return _current_user_id.get()


def set_request_user_groups(user_id: str, groups: str | None) -> None:
    """Store the groups of the user user_id for the current async context.

    The value is passed on unread with the user ID on the calls to the kagent
    controller, which reviews those calls with the groups.
    """
    _current_user_groups.set((user_id, groups) if groups else None)


def get_request_user_groups(user_id: str | None) -> str | None:
    """Return the groups stored for user_id; None for another user's."""
    carried = _current_user_groups.get()
    if not user_id or carried is None or carried[0] != user_id:
        return None
    return carried[1]
