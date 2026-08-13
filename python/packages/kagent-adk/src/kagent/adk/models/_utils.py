from __future__ import annotations

from collections.abc import Mapping
from copy import deepcopy
from typing import Any

from google.genai import types


def _normalize_type_values(value: Any) -> Any:
    if isinstance(value, dict):
        return {
            key: item.lower() if key == "type" and isinstance(item, str) else _normalize_type_values(item)
            for key, item in value.items()
        }
    if isinstance(value, list):
        return [_normalize_type_values(item) for item in value]
    return value


def function_declaration_schema(declaration: types.FunctionDeclaration) -> dict[str, Any]:
    """Return the complete JSON schema for an ADK function declaration.

    ADK 2 MCP tools use ``parameters_json_schema`` while declaration-based
    tools may still use the legacy ``parameters`` model. Prefer the former and
    retain the latter as a fallback, matching the Go model adapters.
    """
    json_schema = declaration.parameters_json_schema
    if isinstance(json_schema, Mapping):
        schema = deepcopy(dict(json_schema))
    elif declaration.parameters is not None:
        schema = declaration.parameters.model_dump(exclude_none=True, by_alias=True, mode="json")
    else:
        schema = {}

    schema = _normalize_type_values(schema)
    schema.setdefault("type", "object")
    if schema["type"] == "object":
        schema.setdefault("properties", {})
    return schema
