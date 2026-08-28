"""Promote allowlisted caller context onto every span of an agent request."""

from __future__ import annotations

import hashlib
import hmac
import json
import os
from dataclasses import dataclass
from typing import Any, Optional

from opentelemetry import baggage
from opentelemetry import context as otel_context

# Allowlist of caller-supplied context keys to promote onto agent spans.
# Unset or empty (the default) disables promotion entirely.
#
# Accepts a comma-separated list of source keys, or a JSON array of strings
# and {from, to, hash} objects.
TRACE_CONTEXT_KEYS_ENV_VAR = "KAGENT_TRACE_CONTEXT_KEYS"

# HMAC key used when a mapping sets hash: hmac-sha256. Required for those
# entries; without it the hashed attribute is skipped rather than emitted
# in plaintext.
TRACE_CONTEXT_HASH_KEY_ENV_VAR = "KAGENT_TRACE_CONTEXT_HASH_KEY"

# Namespaces custom promoted values so they cannot shadow a semantic
# convention attribute such as ``service.name``. Registry names
# (``user.*``, ``enduser.*``, ``session.id``) and names already in the
# ``kagent.`` namespace are left unprefixed; see ``_span_attribute_name``.
CONTEXT_ATTRIBUTE_PREFIX = "kagent.context."

HASH_HMAC_SHA256 = "hmac-sha256"

MAX_CONTEXT_KEYS = 32
MAX_CONTEXT_KEY_LENGTH = 64
MAX_CONTEXT_VALUE_LENGTH = 256


@dataclass(frozen=True)
class _ContextMapping:
    source: str
    attribute: str
    hash: str = ""


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
    attribute names go through ``_span_attribute_name``.

    Args:
      metadata: A2A message metadata as a plain dict (may be ``None``).
      context: OTel context to read baggage from. Defaults to the current one.

    Returns:
      Attribute name to sanitised value. Empty when the allowlist is empty,
      which is the default.
    """
    mappings = _allowed_context_mappings()
    if not mappings:
        return {}

    bag = baggage.get_all(context)
    attributes: dict[str, str] = {}
    for mapping in mappings:
        value = _sanitize_context_value(bag.get(mapping.source))
        if metadata is not None:
            scalar = _scalar_string(metadata.get(mapping.source))
            if scalar is not None:
                value = _sanitize_context_value(scalar)
        if not value:
            continue
        if mapping.hash:
            value = _hash_context_value(value, mapping.hash)
            if not value:
                continue
        else:
            value = _truncate_context_value(value)
        name = _span_attribute_name(mapping.attribute)
        if name in attributes:
            continue
        attributes[name] = value
    return attributes


def _allowed_context_mappings() -> list[_ContextMapping]:
    """Parse the ``KAGENT_TRACE_CONTEXT_KEYS`` allowlist.

    Keys that are empty, over-long, or contain whitespace or control characters
    are dropped, and the list is capped at ``MAX_CONTEXT_KEYS`` so a
    misconfigured allowlist cannot inflate span cardinality without bound.
    """
    raw = os.getenv(TRACE_CONTEXT_KEYS_ENV_VAR, "").strip()
    if not raw:
        return []
    if raw.startswith("["):
        return _cap_mappings(_parse_json_allowlist(raw))
    return _cap_mappings(_parse_comma_allowlist(raw))


def _parse_comma_allowlist(raw: str) -> list[_ContextMapping]:
    mappings: list[_ContextMapping] = []
    for candidate in raw.split(","):
        mapping = _new_context_mapping(candidate.strip(), "", "")
        if mapping is not None:
            mappings.append(mapping)
    return mappings


def _parse_json_allowlist(raw: str) -> list[_ContextMapping]:
    try:
        items = json.loads(raw)
    except json.JSONDecodeError:
        return []
    if not isinstance(items, list):
        return []
    mappings: list[_ContextMapping] = []
    for item in items:
        if isinstance(item, str):
            mapping = _new_context_mapping(item, "", "")
        elif isinstance(item, dict):
            from_key, to_key, hash_alg = item.get("from"), item.get("to"), item.get("hash")
            if from_key is not None and not isinstance(from_key, str):
                mapping = None
            else:
                mapping = _new_context_mapping(
                    from_key or "",
                    to_key if isinstance(to_key, str) else "",
                    hash_alg if isinstance(hash_alg, str) else "",
                )
        else:
            mapping = None
        if mapping is not None:
            mappings.append(mapping)
    return mappings


def _new_context_mapping(from_key: str, to_key: str, hash_alg: str) -> Optional[_ContextMapping]:
    from_key = from_key.strip()
    to_key = to_key.strip()
    hash_alg = hash_alg.strip()
    if not from_key or len(from_key) > MAX_CONTEXT_KEY_LENGTH or not _is_attribute_key(from_key):
        return None
    if not to_key:
        to_key = from_key
    if len(to_key) > MAX_CONTEXT_KEY_LENGTH or not _is_attribute_key(to_key):
        return None
    if hash_alg and hash_alg != HASH_HMAC_SHA256:
        return None
    return _ContextMapping(source=from_key, attribute=to_key, hash=hash_alg)


def _cap_mappings(mappings: list[_ContextMapping]) -> list[_ContextMapping]:
    out: list[_ContextMapping] = []
    seen: set[tuple[str, str, str]] = set()
    for mapping in mappings:
        identity = (mapping.source, mapping.attribute, mapping.hash)
        if identity in seen:
            continue
        seen.add(identity)
        out.append(mapping)
        if len(out) == MAX_CONTEXT_KEYS:
            break
    return out


def _span_attribute_name(name: str) -> str:
    """Return the name written onto the span.

    ``user.*``, ``enduser.*``, and ``session.id`` pass through unprefixed so
    operators can use the semantic convention names. Names already in the
    ``kagent.`` namespace are left as-is. Everything else is placed under
    ``kagent.context.`` so a caller-supplied ``service.name`` cannot shadow
    the real one.
    """
    if _is_registry_attribute(name) or name.startswith("kagent."):
        return name
    return CONTEXT_ATTRIBUTE_PREFIX + name


def _is_registry_attribute(name: str) -> bool:
    return name.startswith("user.") or name.startswith("enduser.") or name == "session.id"


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
    """Drop control characters and trim space so a value cannot forge structure."""
    if not isinstance(value, str):
        return ""
    return "".join(char for char in value if not _is_control(char)).strip()


def _truncate_context_value(value: str) -> str:
    return value[:MAX_CONTEXT_VALUE_LENGTH]


def _hash_context_value(value: str, algorithm: str) -> str:
    """Hash *value* with the requested algorithm.

    Unknown algorithms and a missing HMAC key skip the attribute: never fall
    back to putting the original value on the span.
    """
    if algorithm != HASH_HMAC_SHA256:
        return ""
    key = os.getenv(TRACE_CONTEXT_HASH_KEY_ENV_VAR, "")
    if not key:
        return ""
    return hmac.new(key.encode("utf-8"), value.encode("utf-8"), hashlib.sha256).hexdigest()
