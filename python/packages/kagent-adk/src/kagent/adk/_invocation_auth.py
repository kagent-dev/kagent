"""Invocation-bound HTTP authentication; no credentials enter ADK session state."""

from contextvars import ContextVar
from urllib.parse import urlsplit

import httpx

from ._bearer_token import bearer_token

policy_url: ContextVar[str | None] = ContextVar("kagent_egress_policy_url", default=None)
request_headers: ContextVar[dict[str, str] | None] = ContextVar("kagent_request_headers", default=None)


class InvocationCredentialError(ValueError):
    """The originating invocation has no usable credential."""


def credential_source() -> tuple[str | None, str | None]:
    return policy_url.get(), bearer_token.get()


async def invocation_credential() -> str:
    """Resolve this invocation's credential without consulting durable state."""
    return await resolve_credential(credential_source())


async def resolve_credential(source: tuple[str | None, str | None]) -> str:
    """Use only the caller token captured in this invocation."""
    _, token = source
    if not token or token == "kagent-credential-injected" or len(token) > 65536 or "\r" in token or "\n" in token:
        raise InvocationCredentialError("Invocation credential unavailable")
    return token


async def _targets(endpoint: str):
    url = urlsplit(endpoint)
    if url.scheme not in ("http", "https") or not url.netloc or url.username is not None or url.query or url.fragment:
        raise InvocationCredentialError("Invalid runtime egress policy URL")
    try:
        async with httpx.AsyncClient(timeout=10, follow_redirects=False) as client:
            response = await client.get(endpoint, headers={"Authorization": "Bearer kagent-credential-injected"})
            if response.status_code != 200:
                raise InvocationCredentialError("Runtime egress policy unavailable")
            return response.json()
    except httpx.HTTPError:
        raise InvocationCredentialError("Runtime egress policy unavailable") from None


def _origin(url) -> str:
    parsed = urlsplit(str(url))
    host = (parsed.hostname or "").removesuffix(".")
    if ":" in host:
        host = f"[{host}]"
    port = parsed.port if parsed.port is not None else (443 if parsed.scheme == "https" else 80)
    return f"{parsed.scheme}://{host}:{port}"


def authentication_hook(bindings, *, snapshot: bool = False):
    """MCP transports snapshot their opener; model calls use the current task."""
    source = credential_source() if snapshot else None
    headers = [(binding["name"], binding.get("prefix", "")) for binding in bindings]

    async def authenticate(request) -> None:
        current = source if source is not None else credential_source()
        selected = headers
        if current[0]:
            try:
                targets = await _targets(current[0])
                target = next((target for target in targets if target["origin"] == _origin(request.url)), None)
                if target is None:
                    if headers:
                        raise InvocationCredentialError("Invocation authentication binding unavailable")
                    return
                for name, value in target.get("headers", {}).items():
                    request.headers[name] = value
                selected = [(header["name"], header.get("prefix", "")) for header in target.get("passthrough", [])]
            except (ValueError, KeyError, TypeError):
                raise InvocationCredentialError("Invocation authentication configuration unavailable") from None
        if selected:
            token = await resolve_credential(current)
            for name, prefix in selected:
                request.headers[name] = prefix + token

    return authenticate
