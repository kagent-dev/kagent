"""Context variables for request-scoped data."""

from contextvars import ContextVar
from typing import Dict

# Context variable to store HTTP request headers for the current request
_request_headers_var: ContextVar[Dict[str, str]] = ContextVar("request_headers", default=None)


def set_request_headers(headers: Dict[str, str]) -> None:
    """Store request headers in the current context.

    Args:
        headers: Dictionary of HTTP request headers
    """
    _request_headers_var.set(headers)


def get_request_headers() -> Dict[str, str]:
    """Get request headers from the current context.

    Returns:
        Dictionary of HTTP request headers, or empty dict if not set
    """
    return _request_headers_var.get()


def clear_request_headers() -> None:
    """Clear request headers from the current context."""
    _request_headers_var.set({})
