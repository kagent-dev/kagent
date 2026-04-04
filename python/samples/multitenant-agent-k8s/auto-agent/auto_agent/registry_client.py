"""HTTP client for tool-registry service.

GET  /tools?tenant_id=X&org_id=Y  → list[ToolInfo]   (with TTL cache)
POST /call/{name}                  → dict              (with timeout)
"""
from __future__ import annotations

import asyncio
import logging
import os
import time
from dataclasses import dataclass, field

import httpx

log = logging.getLogger(__name__)

TOOL_REGISTRY_URL = os.getenv(
    "TOOL_REGISTRY_URL",
    "http://tool-registry.platform.svc.cluster.local:8080",
)
REGISTRY_TOKEN = os.getenv("REGISTRY_TOKEN", "")
CALL_TIMEOUT = 30  # seconds
_CACHE_TTL = 60  # seconds


@dataclass
class ToolInfo:
    name: str
    description: str
    input_schema: dict
    endpoint_url: str
    tenant_id: str | None
    org_id: str | None
    scopes: list[str] = field(default_factory=list)
    version: str = "v1"


# ── in-process cache ─────────────────────────────────────────────────────────
_cache: dict[str, tuple[list[ToolInfo], float]] = {}  # key → (data, timestamp)
_cache_lock = asyncio.Lock()


def _cache_key(tenant_id: str, org_id: str) -> str:
    return f"{tenant_id}:{org_id}"


async def get_tools(tenant_id: str, org_id: str) -> list[ToolInfo]:
    """Return tools available for tenant+org. TTL-cached for 60 s."""
    key = _cache_key(tenant_id, org_id)

    async with _cache_lock:
        cached = _cache.get(key)
        if cached and time.monotonic() - cached[1] < _CACHE_TTL:
            return cached[0]

    headers = {"Authorization": f"Bearer {REGISTRY_TOKEN}"} if REGISTRY_TOKEN else {}
    try:
        async with httpx.AsyncClient(timeout=10, headers=headers) as client:
            r = await client.get(
                f"{TOOL_REGISTRY_URL}/tools",
                params={"tenant_id": tenant_id, "org_id": org_id},
            )
            r.raise_for_status()
            data = r.json()
    except Exception as exc:
        log.warning("registry_client.get_tools failed: %s", exc)
        # Return stale cache on error rather than failing the agent
        async with _cache_lock:
            stale = _cache.get(key)
            if stale:
                log.warning("Returning stale tool list for %s", key)
                return stale[0]
        return []

    tools = [ToolInfo(**item) for item in data]
    async with _cache_lock:
        _cache[key] = (tools, time.monotonic())

    return tools


async def call_tool(
    tool_name: str,
    arguments: dict,
    tenant_id: str,
    org_id: str,
) -> dict:
    """Proxy a tool call through tool-registry with scope check + timeout."""
    headers = {"Authorization": f"Bearer {REGISTRY_TOKEN}"} if REGISTRY_TOKEN else {}
    payload = {"arguments": arguments, "org_id": org_id}

    try:
        async with httpx.AsyncClient(
            timeout=CALL_TIMEOUT, headers=headers
        ) as client:
            r = await client.post(
                f"{TOOL_REGISTRY_URL}/call/{tool_name}",
                json=payload,
            )
            if r.status_code == 403:
                return {"error": f"Tool '{tool_name}' not available for org '{org_id}'"}
            r.raise_for_status()
            return r.json()
    except httpx.TimeoutException:
        return {"error": f"Tool '{tool_name}' timed out after {CALL_TIMEOUT}s"}
    except Exception as exc:
        log.error("registry_client.call_tool(%s) failed: %s", tool_name, exc)
        return {"error": str(exc)}


def invalidate_cache(tenant_id: str = "", org_id: str = "") -> None:
    """Force cache refresh on next call. Pass empty strings to clear all."""
    if tenant_id or org_id:
        _cache.pop(_cache_key(tenant_id, org_id), None)
    else:
        _cache.clear()
