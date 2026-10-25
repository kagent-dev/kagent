"""Invocation-bound HTTP authentication; no credentials enter ADK session state."""

from contextvars import ContextVar
from urllib.parse import urlsplit

from pydantic import BaseModel, Field, TypeAdapter

from ._bearer_token import bearer_token

HEADERS_METADATA_KEY = "x-kagent-invocation-headers-bin"


class InvocationHeader(BaseModel):
    name: str
    prefix: str = ""


class InvocationTarget(BaseModel):
    origin: str
    headers: dict[str, str] = Field(default_factory=dict)
    passthrough: list[InvocationHeader] = Field(default_factory=list)


invocation_targets: ContextVar[list[InvocationTarget] | None] = ContextVar("kagent_invocation_targets", default=None)
request_headers: ContextVar[dict[str, str] | None] = ContextVar("kagent_request_headers", default=None)


class InvocationCredentialError(ValueError):
    """The originating invocation has no usable credential."""


def parse_targets(headers: dict) -> list[InvocationTarget] | None:
    """Read the control plane's snapshot from private A2A metadata once per invocation."""
    value = headers.get(HEADERS_METADATA_KEY)
    if value is None:
        return None
    try:
        return TypeAdapter(list[InvocationTarget]).validate_json(value)
    except (ValueError, TypeError):
        raise InvocationCredentialError("Invalid invocation header bindings") from None


def credential_source() -> tuple[list[InvocationTarget] | None, str | None]:
    return invocation_targets.get(), bearer_token.get()


async def invocation_credential() -> str:
    """Resolve this invocation's credential without consulting durable state."""
    return await resolve_credential(credential_source())


async def resolve_credential(source: tuple[list[InvocationTarget] | None, str | None]) -> str:
    """Use only the caller token captured in this invocation."""
    _, token = source
    if not token or token == "kagent-credential-injected" or len(token) > 65536 or "\r" in token or "\n" in token:
        raise InvocationCredentialError("Invocation credential unavailable")
    return token


def _origin(url) -> str:
    parsed = urlsplit(str(url))
    host = (parsed.hostname or "").removesuffix(".")
    if ":" in host:
        host = f"[{host}]"
    port = parsed.port if parsed.port is not None else (443 if parsed.scheme == "https" else 80)
    return f"{parsed.scheme}://{host}:{port}"


def authentication_hook(*, snapshot: bool = False):
    """MCP transports snapshot their opener; model calls use the current task."""
    source = credential_source() if snapshot else None

    async def authenticate(request) -> None:
        current = source if source is not None else credential_source()
        target = next((target for target in (current[0] or []) if target.origin == _origin(request.url)), None)
        if target is None:
            return
        for name, value in target.headers.items():
            request.headers[name] = value
        if target.passthrough:
            token = await resolve_credential(current)
            for header in target.passthrough:
                request.headers[header.name] = header.prefix + token

    return authenticate
