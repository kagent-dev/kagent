from __future__ import annotations

import json
from collections.abc import Mapping
from typing import Any

from google.protobuf import json_format, struct_pb2
from kagent.api.v1alpha1 import common_pb2


def encode_structured_object(
    value: Mapping[str, Any],
    *,
    api_version: str,
    kind: str,
    max_bytes: int,
) -> common_pb2.StructuredObject:
    if not kind:
        raise ValueError("structured object kind is empty")
    payload = dict(value)
    _check_size(payload, max_bytes)
    protobuf_value = struct_pb2.Struct()
    json_format.ParseDict(payload, protobuf_value)
    return common_pb2.StructuredObject(api_version=api_version, kind=kind, value=protobuf_value)


def decode_structured_object(
    value: common_pb2.StructuredObject,
    *,
    expected_kind: str,
    max_bytes: int,
) -> dict[str, Any]:
    if value is None or not value.HasField("value"):
        raise ValueError("structured object value is missing")
    if expected_kind and value.kind != expected_kind:
        raise ValueError(f"structured object kind {value.kind!r} does not match {expected_kind!r}")
    payload = json_format.MessageToDict(value.value)
    if not isinstance(payload, dict):
        raise ValueError("structured object root is not an object")
    _check_size(payload, max_bytes)
    return payload


def _check_size(value: Mapping[str, Any], max_bytes: int) -> None:
    encoded = json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(",", ":")).encode()
    if max_bytes > 0 and len(encoded) > max_bytes:
        raise ValueError(f"structured object exceeds configured size limit: got {len(encoded)} bytes, limit {max_bytes}")
