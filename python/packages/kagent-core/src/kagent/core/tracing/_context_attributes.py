"""Promote allowlisted caller context onto every span of an agent request."""

import os
from typing import Any, Optional

from opentelemetry import baggage
from opentelemetry import context as otel_context

# Comma-separated allowlist of caller-supplied context keys to promote onto
# agent spans. Unset or empty (the default) disables promotion entirely.
TRACE_CONTEXT_KEYS_ENV_VAR = "KAGENT_TRACE_CONTEXT_KEYS"

# Namespaces every promoted value. Because the prefix is applied
# unconditionally, caller-supplied data cannot shadow a semantic convention
# attribute such as ``service.name``.
CONTEXT_ATTRIBUTE_PREFIX = "kagent.context."

MAX_CONTEXT_KEYS = 32
MAX_CONTEXT_KEY_LENGTH = 64
MAX_CONTEXT_VALUE_LENGTH = 256


def caller_context_attributes(
    metadata: Optional[dict[str, Any]] = None,
    context: Optional[otel_context.Context] = None,
) -> dict[str, str]:
    """Return the caller context values an operator allowlisted for tracing.

    Merge the result into the request-scoped attribute bag (see
    ``set_kagent_span_attributes``) so every span of the request carries the
    values: trace-level filtering in backends such as Langfuse matches on each
    span, not only on the root.

    Values are read from W3C baggage first and then from the A2A message
    metadata, which is the more specific source for a single message and
    therefore wins. Both are untrusted input, so keys must appear in the
    allowlist, values are stripped of control characters and truncated, and
    every attribute is namespaced under ``CONTEXT_ATTRIBUTE_PREFIX``.

    Args:
      metadata: A2A message metadata as a plain dict (may be ``None``).
      context: OTel context to read baggage from. Defaults to the current one.

    Returns:
      Prefixed attribute name to sanitised value. Empty when the allowlist is
      empty, which is the default.
    """
    keys = _allowed_context_keys()
    if not keys:
        return {}

    bag = baggage.get_all(context)
    attributes: dict[str, str] = {}
    for key in keys:
        value = _sanitize_context_value(bag.get(key))
        if metadata is not None:
            scalar = _scalar_string(metadata.get(key))
            if scalar is not None:
                value = _sanitize_context_value(scalar)
        if not value:
            continue
        attributes[CONTEXT_ATTRIBUTE_PREFIX + key] = value
    return attributes


def _allowed_context_keys() -> list[str]:
    """Parse the ``KAGENT_TRACE_CONTEXT_KEYS`` allowlist.

    Keys that are empty, over-long, or contain whitespace or control characters
    are dropped, and the list is capped at ``MAX_CONTEXT_KEYS`` so a
    misconfigured allowlist cannot inflate span cardinality without bound.
    """
    raw = os.getenv(TRACE_CONTEXT_KEYS_ENV_VAR, "").strip()
    if not raw:
        return []

    keys: list[str] = []
    for candidate in raw.split(","):
        key = candidate.strip()
        if not key or len(key) > MAX_CONTEXT_KEY_LENGTH or not _is_attribute_key(key):
            continue
        if key in keys:
            continue
        keys.append(key)
        if len(keys) == MAX_CONTEXT_KEYS:
            break
    return keys


def _is_attribute_key(key: str) -> bool:
    """Report whether *key* is safe to use as a span attribute name."""
    return not any(_is_control(char) or char.isspace() for char in key)


def _is_control(char: str) -> bool:
    """Match Go's ``unicode.IsControl`` so both runtimes sanitise identically."""
    code_point = ord(char)
    return code_point < 0x20 or 0x7F <= code_point <= 0x9F


def _scalar_string(value: Any) -> Optional[str]:
    """Render a JSON scalar from A2A message metadata as a string.

    Objects and arrays are skipped: they are unbounded in size and carry no
    useful meaning as a span attribute value. ``None`` means "no scalar here",
    which leaves any baggage value for the same key in place.
    """
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        # protobuf Struct has a single numeric type, so a JSON integer arrives
        # as a float. Render it without the trailing ".0" to match the Go ADK.
        return str(int(value)) if value.is_integer() else repr(value)
    if isinstance(value, str):
        return value
    return None


def _sanitize_context_value(value: Any) -> str:
    """Make an untrusted value safe to attach to a span.

    Control characters are dropped so a value cannot forge structure in a
    downstream trace or log renderer, and the result is truncated to bound the
    size of exported spans.
    """
    if not isinstance(value, str):
        return ""
    cleaned = "".join(char for char in value if not _is_control(char)).strip()
    return cleaned[:MAX_CONTEXT_VALUE_LENGTH]
