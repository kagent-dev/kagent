"""Public request identity, independent of the snapshot's private session keys."""

from contextvars import ContextVar

public_context_id: ContextVar[str] = ContextVar("public_context_id", default="")
request_user_id: ContextVar[str] = ContextVar("request_user_id", default="")
