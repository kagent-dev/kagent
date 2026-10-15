"""The incoming A2A request's headers in ADK session state.

A turn carries its request headers in two state keys:

- ``temp:headers`` holds every header, credentials included. ADK keeps
  ``temp:`` keys in the in-memory session for the current invocation only and
  never writes them to a session service, so a bearer token is readable by the
  plugins, header providers and tools of the turn it arrived with and never
  reaches a session database.
- ``headers`` holds the same headers without the credential-bearing ones and
  is persisted with the session like any other state.

Readers that need a credential use ``request_headers(state)``.
"""

from collections.abc import Mapping
from typing import Any

HEADERS_STATE_KEY = "headers"
REQUEST_HEADERS_STATE_KEY = "temp:headers"

_CREDENTIAL_HEADERS = frozenset({"authorization", "proxy-authorization", "cookie", "set-cookie"})
_CREDENTIAL_NAME_PARTS = ("auth", "token", "secret", "password", "api-key", "apikey", "credential", "cookie")


def is_credential_header(name: str) -> bool:
    """Whether a header may carry a credential and must never be persisted."""
    name = name.lower()
    return name in _CREDENTIAL_HEADERS or any(part in name for part in _CREDENTIAL_NAME_PARTS)


def without_credentials(headers: Mapping[str, Any]) -> dict[str, Any]:
    """The headers without the credential-bearing ones."""
    return {name: value for name, value in headers.items() if not is_credential_header(name)}


def headers_state_delta(headers: Mapping[str, Any]) -> dict[str, Any]:
    """The state delta that hands a turn its request headers."""
    return {
        HEADERS_STATE_KEY: without_credentials(headers),
        REQUEST_HEADERS_STATE_KEY: dict(headers),
    }


def request_headers(state: Mapping[str, Any]) -> Mapping[str, Any]:
    """The current request's headers, credentials included, from session state."""
    headers = state.get(REQUEST_HEADERS_STATE_KEY)
    if not isinstance(headers, Mapping):
        headers = state.get(HEADERS_STATE_KEY)
    return headers if isinstance(headers, Mapping) else {}
