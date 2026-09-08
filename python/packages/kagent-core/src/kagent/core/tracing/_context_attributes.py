"""Promote allowlisted caller context into W3C baggage."""

from __future__ import annotations

import functools
import json
import os
from contextvars import Token
from dataclasses import dataclass
from typing import Any, Callable, Optional

from a2a.types import Message
from opentelemetry import baggage
from opentelemetry import context as otel_context

from kagent.core.a2a._consts import read_message_metadata

# Allowlist of caller-supplied context keys copied from W3C baggage onto
# agent spans. Unset or empty (the default) disables promotion entirely.
#
# Accepts a comma-separated list of keys, or a JSON array of strings and
# {from, to} objects. Destination names are used as-is.
TRACE_CONTEXT_KEYS_ENV_VAR = "KAGENT_TRACE_CONTEXT_KEYS"

MAX_CONTEXT_KEYS = 32
MAX_CONTEXT_KEY_LENGTH = 64


@dataclass(frozen=True)
class _ContextMapping:
    source: str
    attribute: str


def context_with_promoted_metadata(
    metadata: Optional[dict[str, Any]] = None,
    context: Optional[otel_context.Context] = None,
    message: Optional[Message] = None,
) -> otel_context.Context:
    """Copy allowlisted A2A metadata into baggage when the destination is unset.

    Existing baggage members named in ``from`` are remapped onto ``to`` the
    same way, so both sources share one path: the baggage span processor.

    ``message`` is decoded only after the allowlist is known to be non-empty.
    Empty or non-scalar metadata does not wipe baggage.
    """
    mappings = _allowed_context_mappings()
    ctx = context if context is not None else otel_context.get_current()
    if not mappings:
        return ctx
    if metadata is None and message is not None:
        metadata = read_message_metadata(message)
    if metadata is None:
        metadata = {}

    bag = baggage.get_all(ctx)
    for mapping in mappings:
        existing = bag.get(mapping.attribute)
        if isinstance(existing, str) and existing.strip():
            continue
        value = ""
        scalar = _scalar_string(metadata.get(mapping.source))
        if scalar is not None:
            value = _sanitize_context_value(scalar)
        if not value and mapping.source != mapping.attribute:
            value = _sanitize_context_value(bag.get(mapping.source))
        if not value:
            continue
        ctx = baggage.set_baggage(mapping.attribute, value, context=ctx)
        bag = baggage.get_all(ctx)
    return ctx


def promote_message_metadata_to_baggage(
    metadata: Optional[dict[str, Any]] = None,
    context: Optional[otel_context.Context] = None,
    message: Optional[Message] = None,
) -> Optional[Token[otel_context.Context]]:
    """Attach a context with allowlisted metadata written into baggage.

    Returns a token that must be detached with
    :func:`detach_promoted_metadata`, or ``None`` when nothing changed.
    """
    attached = otel_context.get_current()
    source = context if context is not None else attached
    promoted = context_with_promoted_metadata(metadata, source, message)
    if promoted is attached:
        return None
    return otel_context.attach(promoted)


def detach_promoted_metadata(token: Optional[Token[otel_context.Context]]) -> None:
    """Detach a token returned by :func:`promote_message_metadata_to_baggage`."""
    if token is not None:
        otel_context.detach(token)


def allowed_baggage_key_predicate() -> Optional[Callable[[str], bool]]:
    """Return a BaggageSpanProcessor predicate, or ``None`` when promotion is off."""
    mappings = _allowed_context_mappings()
    if not mappings:
        return None
    allowed = frozenset(mapping.attribute for mapping in mappings)
    return allowed.__contains__


@functools.cache
def _allowed_context_mappings() -> tuple[_ContextMapping, ...]:
    """Parse the ``KAGENT_TRACE_CONTEXT_KEYS`` allowlist.

    Keys that are empty, over-long, or contain whitespace or control characters
    are dropped, and the list is capped at ``MAX_CONTEXT_KEYS``.
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
        mapping = _new_context_mapping(candidate.strip(), "")
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
            mapping = _new_context_mapping(item, "")
        elif isinstance(item, dict):
            # Leftover {hash:...} config is no longer supported. Drop the
            # mapping rather than stamping the raw source value onto `to`.
            if "hash" in item:
                mapping = None
            else:
                from_key, from_ok = _json_string_field(item, "from")
                to_key, to_ok = _json_string_field(item, "to")
                if not from_ok or not to_ok:
                    mapping = None
                else:
                    mapping = _new_context_mapping(from_key, to_key)
        else:
            mapping = None
        if mapping is not None:
            mappings.append(mapping)
    return mappings


def _json_string_field(item: dict[str, Any], name: str) -> tuple[str, bool]:
    """Return a JSON string field, or reject it when the type is invalid."""
    if name not in item:
        return "", True
    raw = item[name]
    if raw is None:
        return "", True
    if isinstance(raw, str):
        return raw, True
    return "", False


def _new_context_mapping(from_key: str, to_key: str) -> Optional[_ContextMapping]:
    from_key = from_key.strip()
    to_key = to_key.strip()
    if not from_key or len(from_key) > MAX_CONTEXT_KEY_LENGTH or not _is_attribute_key(from_key):
        return None
    if not to_key:
        to_key = from_key
    if len(to_key) > MAX_CONTEXT_KEY_LENGTH or not _is_attribute_key(to_key):
        return None
    return _ContextMapping(source=from_key, attribute=to_key)


def _cap_mappings(mappings: list[_ContextMapping]) -> list[_ContextMapping]:
    out: list[_ContextMapping] = []
    seen: set[tuple[str, str]] = set()
    for mapping in mappings:
        identity = (mapping.source, mapping.attribute)
        if identity in seen:
            continue
        seen.add(identity)
        out.append(mapping)
        if len(out) == MAX_CONTEXT_KEYS:
            break
    return out


def _is_attribute_key(key: str) -> bool:
    return not any(_is_control(char) or char.isspace() for char in key)


def _is_control(char: str) -> bool:
    """Match Go's ``unicode.IsControl`` so both runtimes sanitise identically."""
    code_point = ord(char)
    return code_point < 0x20 or 0x7F <= code_point <= 0x9F


def _scalar_string(value: Any) -> Optional[str]:
    """Render a JSON scalar from A2A message metadata as a string."""
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
    """Match Go ``strconv.FormatFloat(v, 'f', -1, 64)``: never scientific notation."""
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
