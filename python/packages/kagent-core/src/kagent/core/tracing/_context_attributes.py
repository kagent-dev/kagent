"""Promote allowlisted caller context onto every span of an agent request."""

from __future__ import annotations

import functools
import hashlib
import hmac
import json
import os
from dataclasses import dataclass
from typing import Any, Optional

from a2a.types import Message
from opentelemetry import baggage
from opentelemetry import context as otel_context

from kagent.core.a2a._consts import read_message_metadata

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
# convention attribute such as ``service.name``. Only the explicit registry
# names in ``_UNPREFIXED_ATTRIBUTES`` pass through as-is; see
# ``_span_attribute_name``.
CONTEXT_ATTRIBUTE_PREFIX = "kagent.context."

HASH_HMAC_SHA256 = "hmac-sha256"

MAX_CONTEXT_KEYS = 32
MAX_CONTEXT_KEY_LENGTH = 64
MAX_CONTEXT_VALUE_LENGTH = 256

# OpenTelemetry registry names callers may emit without a kagent.context.
# prefix. An invented name such as user.asdasd cannot masquerade as a
# semantic convention.
_UNPREFIXED_ATTRIBUTES = frozenset({"user.id", "user.hash", "enduser.id", "session.id"})


@dataclass(frozen=True)
class _ContextMapping:
    source: str
    attribute: str
    hash: str = ""


def caller_context_attributes(
    metadata: Optional[dict[str, Any]] = None,
    context: Optional[otel_context.Context] = None,
    message: Optional[Message] = None,
) -> dict[str, str]:
    """Return the caller context values an operator allowlisted for tracing.

    Merge the result into the request-scoped attribute bag (see
    ``set_kagent_span_attributes``) so every span of the request carries the
    values: trace-level filtering in backends such as Langfuse matches on each
    span, not only on the root.

    Values are read from W3C baggage first and then from the A2A message
    metadata, which is the more specific source for a single message and
    therefore wins — but only when the sanitised metadata scalar is non-empty,
    so a blank metadata entry cannot wipe a baggage value. Both are untrusted
    input, so keys must appear in the allowlist, values are stripped of control
    characters and truncated, and attribute names go through
    ``_span_attribute_name``.

    ``message`` is decoded only after the allowlist is known to be non-empty,
    so the default (promotion off) does not pay for protobuf Struct conversion.

    Args:
      metadata: A2A message metadata as a plain dict (may be ``None``).
      context: OTel context to read baggage from. Defaults to the current one.
      message: A2A message whose metadata is decoded lazily when ``metadata``
        is ``None`` and the allowlist is non-empty.

    Returns:
      Attribute name to sanitised value. Empty when the allowlist is empty,
      which is the default.
    """
    mappings = _allowed_context_mappings()
    if not mappings:
        return {}
    if metadata is None and message is not None:
        metadata = read_message_metadata(message)

    bag = baggage.get_all(context)
    attributes: dict[str, str] = {}
    for mapping in mappings:
        value = _sanitize_context_value(bag.get(mapping.source))
        if metadata is not None:
            scalar = _scalar_string(metadata.get(mapping.source))
            if scalar is not None:
                cleaned = _sanitize_context_value(scalar)
                if cleaned:
                    value = cleaned
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


def merge_caller_context_attributes(
    existing: dict[str, Any],
    metadata: Optional[dict[str, Any]] = None,
    context: Optional[otel_context.Context] = None,
    message: Optional[Message] = None,
) -> None:
    """Copy promoted caller context into *existing* without replacing keys.

    Caller-supplied data cannot override ``kagent.user_id``, ``kagent.app_name``,
    or other attributes the runtime already stamped.
    """
    for key, value in caller_context_attributes(metadata, context, message=message).items():
        if key not in existing:
            existing[key] = value


@functools.cache
def _allowed_context_mappings() -> tuple[_ContextMapping, ...]:
    """Parse the ``KAGENT_TRACE_CONTEXT_KEYS`` allowlist.

    Keys that are empty, over-long, or contain whitespace or control characters
    are dropped, and the list is capped at ``MAX_CONTEXT_KEYS`` so a
    misconfigured allowlist cannot inflate span cardinality without bound.
    Cached because the allowlist cannot change without a process restart.
    """
    raw = os.getenv(TRACE_CONTEXT_KEYS_ENV_VAR, "").strip()
    if not raw:
        return ()
    if raw.startswith("["):
        return tuple(_cap_mappings(_parse_json_allowlist(raw)))
    return tuple(_cap_mappings(_parse_comma_allowlist(raw)))


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
            from_key, from_ok = _json_string_field(item, "from")
            to_key, to_ok = _json_string_field(item, "to")
            hash_alg, hash_ok = _json_string_field(item, "hash")
            if not from_ok or not to_ok or not hash_ok:
                # Non-string hash/to must drop the mapping. Coercing hash: 123
                # to "no hash" would emit the original value in plaintext.
                mapping = None
            else:
                mapping = _new_context_mapping(from_key, to_key, hash_alg)
        else:
            mapping = None
        if mapping is not None:
            mappings.append(mapping)
    return mappings


def _json_string_field(item: dict[str, Any], name: str) -> tuple[str, bool]:
    """Return a JSON string field, or reject it when the type is invalid.

    Missing keys and JSON null are unset (empty string), matching Go's
    encoding/json behaviour for string fields. Any other non-string type is
    a malformed mapping and must fail closed.
    """
    if name not in item:
        return "", True
    raw = item[name]
    if raw is None:
        return "", True
    if isinstance(raw, str):
        return raw, True
    return "", False


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

    ``user.id``, ``user.hash``, ``enduser.id``, and ``session.id`` pass through
    unprefixed so operators can use the semantic convention names. Everything
    else is placed under ``kagent.context.`` so a caller-supplied
    ``service.name`` or ``kagent.user_id`` cannot shadow the real one.
    """
    if name in _UNPREFIXED_ATTRIBUTES:
        return name
    return CONTEXT_ATTRIBUTE_PREFIX + name


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
        return _format_float(value)
    if isinstance(value, str):
        return value
    return None


def _format_float(value: float) -> str:
    """Match Go ``strconv.FormatFloat(v, 'f', -1, 64)``: never scientific notation.

    ``format(value, '.16f')`` rounds values such as ``1e-20`` to ``0``. Expand
    Python's shortest unique repr from scientific form into fixed-point
    instead, so tiny values keep their digits.
    """
    return _fixed_point_from_shortest(repr(value))


def _fixed_point_from_shortest(text: str) -> str:
    lower = text.lower()
    negative = lower.startswith("-")
    if negative:
        lower = lower[1:]
    if "e" not in lower:
        if "." in lower:
            lower = lower.rstrip("0").rstrip(".")
        out = lower or "0"
        return f"-{out}" if negative and out != "0" else out
    mantissa, exp_s = lower.split("e")
    exp = int(exp_s)
    if "." in mantissa:
        whole, frac = mantissa.split(".")
        digits = whole + frac
        exp -= len(frac)
    else:
        digits = mantissa
    digits = digits.lstrip("0") or "0"
    if exp >= 0:
        out = digits + "0" * exp
    else:
        shift = -exp
        if len(digits) <= shift:
            out = "0." + "0" * (shift - len(digits)) + digits
        else:
            idx = len(digits) - shift
            frac = digits[idx:].rstrip("0")
            out = digits[:idx] + (("." + frac) if frac else "")
    return f"-{out}" if negative and out != "0" else out


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
