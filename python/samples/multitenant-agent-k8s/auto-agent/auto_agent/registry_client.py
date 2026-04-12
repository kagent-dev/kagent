"""HTTP client for tool-registry service.

GET  /tools?tenant_id=X&org_id=Y  → list[ToolInfo]   (TTL-cached)
POST /call/{name}                  → dict              (timeout enforced)

Connection pooling:
    A single persistent AsyncClient is shared across all calls.
    This avoids TCP handshake overhead on every sequential call and is
    required for parallel calls (call_tools_parallel) to avoid spawning
    an unbounded number of connections.
    Pool limits: max 20 connections (10 per host) — enough for heavy parallel use.
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

# ── Shared persistent HTTP client ─────────────────────────────────────────────
# One client for the process lifetime — reuses TCP connections for all calls.
# Limits: 20 total / 10 per host — enough for parallel batch calls.
_http_client: httpx.AsyncClient | None = None
_http_client_lock = asyncio.Lock()


def _auth_headers() -> dict[str, str]:
    return {"Authorization": f"Bearer {REGISTRY_TOKEN}"} if REGISTRY_TOKEN else {}


async def _get_client() -> httpx.AsyncClient:
    global _http_client
    if _http_client is None or _http_client.is_closed:
        async with _http_client_lock:
            if _http_client is None or _http_client.is_closed:
                limits = httpx.Limits(max_connections=20, max_keepalive_connections=10)
                _http_client = httpx.AsyncClient(
                    base_url=TOOL_REGISTRY_URL,
                    headers=_auth_headers(),
                    limits=limits,
                    timeout=httpx.Timeout(CALL_TIMEOUT, connect=5.0),
                )
                log.debug("Created persistent httpx.AsyncClient for tool-registry")
    return _http_client


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
    """Return tools available for tenant+org (TTL-cached for 60 s)."""
    key = _cache_key(tenant_id, org_id)

    async with _cache_lock:
        cached = _cache.get(key)
        if cached and time.monotonic() - cached[1] < _CACHE_TTL:
            return cached[0]

    client = await _get_client()
    try:
        r = await client.get(
            "/tools",
            params={"tenant_id": tenant_id, "org_id": org_id},
            timeout=10,
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
    """Proxy a tool call to tool-registry. Enforces tenant+org scope and timeout."""
    encoded_name = tool_name.replace("/", "%2F").replace("?", "%3F")
    payload = {"arguments": arguments, "tenant_id": tenant_id, "org_id": org_id}

    client = await _get_client()
    try:
        r = await client.post(f"/call/{encoded_name}", json=payload)
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
