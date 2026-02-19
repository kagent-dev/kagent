"""Centralized environment variable registry for kagent Python packages.

Variables are self-registering: calling any ``register_*`` function records the
variable's metadata (name, default, description, type, component) in a
process-wide registry and returns the resolved value.

This mirrors the Go ``pkg/env`` package so both sides of the codebase have a
single, documentable source of truth for every environment variable.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from typing import Optional


@dataclass(frozen=True)
class EnvVar:
    """Metadata for a registered environment variable."""

    name: str
    default: Optional[str]
    description: str
    var_type: str  # "string", "bool", "int", "float"
    component: str  # "core", "adk", "skills", "openai", "crewai", "agent-runtime"
    deprecated: bool = field(default=False)
    hidden: bool = field(default=False)


_registry: dict[str, EnvVar] = {}


def _register(var: EnvVar) -> None:
    _registry[var.name] = var


# ---------------------------------------------------------------------------
# Typed registration helpers
# ---------------------------------------------------------------------------


def register_string(
    name: str,
    default: Optional[str],
    description: str,
    component: str,
    *,
    deprecated: bool = False,
    hidden: bool = False,
) -> Optional[str]:
    """Register a string environment variable and return its current value."""
    _register(
        EnvVar(
            name=name,
            default=default,
            description=description,
            var_type="string",
            component=component,
            deprecated=deprecated,
            hidden=hidden,
        )
    )
    val = os.environ.get(name)
    if val is not None:
        return val
    return default


def register_bool(
    name: str,
    default: bool,
    description: str,
    component: str,
    *,
    deprecated: bool = False,
    hidden: bool = False,
) -> bool:
    """Register a boolean environment variable and return its current value."""
    _register(
        EnvVar(
            name=name,
            default=str(default).lower(),
            description=description,
            var_type="bool",
            component=component,
            deprecated=deprecated,
            hidden=hidden,
        )
    )
    val = os.environ.get(name)
    if val is not None:
        return val.lower() in ("true", "1", "yes")
    return default


def register_int(
    name: str,
    default: int,
    description: str,
    component: str,
    *,
    deprecated: bool = False,
    hidden: bool = False,
) -> int:
    """Register an integer environment variable and return its current value."""
    _register(
        EnvVar(
            name=name,
            default=str(default),
            description=description,
            var_type="int",
            component=component,
            deprecated=deprecated,
            hidden=hidden,
        )
    )
    val = os.environ.get(name)
    if val is not None:
        try:
            return int(val)
        except ValueError:
            return default
    return default


# ---------------------------------------------------------------------------
# Accessors
# ---------------------------------------------------------------------------


def all_vars() -> list[EnvVar]:
    """Return all registered variables sorted by name."""
    return sorted(_registry.values(), key=lambda v: v.name)


def get_var(name: str) -> Optional[EnvVar]:
    """Return the metadata for a registered variable, or None."""
    return _registry.get(name)


# ---------------------------------------------------------------------------
# Export helpers
# ---------------------------------------------------------------------------


def export_markdown(component: Optional[str] = None) -> str:
    """Generate a markdown document listing all registered variables."""
    vars_list = all_vars()
    lines: list[str] = ["# Kagent Environment Variables (Python)\n"]

    grouped: dict[str, list[EnvVar]] = {}
    for v in vars_list:
        if v.hidden:
            continue
        if component and component != "all" and v.component != component:
            continue
        grouped.setdefault(v.component, []).append(v)

    for comp in sorted(grouped):
        lines.append(f"\n## {comp}\n")
        lines.append("| Variable | Type | Default | Description |")
        lines.append("|----------|------|---------|-------------|")
        for v in grouped[comp]:
            dep = " **(deprecated)**" if v.deprecated else ""
            default_val = v.default if v.default is not None else "(none)"
            lines.append(f"| `{v.name}` | {v.var_type} | `{default_val}` | {v.description}{dep} |")

    lines.append("")
    return "\n".join(lines)


def reset_for_testing() -> None:
    """Clear all registered variables. Only for use in tests."""
    _registry.clear()
