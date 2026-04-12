"""CallToolsParallelTool — execute multiple tools simultaneously.

"Many hands per brain" pattern: the agent dispatches N independent tool calls
at the same time and collects all results before reasoning over them.

When to use vs call_tool (sequential):
    call_tool          — one tool at a time; use when result of A feeds into B
    call_tools_parallel — all calls are independent; use when you need to gather
                          state from multiple sources simultaneously (e.g. get_pods
                          + get_metrics + get_logs for the same incident at once)

Behaviour:
    - Calls are dispatched concurrently via asyncio.gather
    - Each call is individually scoped (tenant+org) and has its own timeout
    - A failure in one call does NOT cancel the others — errors are returned
      inline as {"error": "..."} in the corresponding result slot
    - Max 10 calls per invocation (prevent accidental DoS to tool-registry)
    - ask_human is still required if ANY of the calls performs a write action

Result formatting (pattern from masmcp WorkflowBuilder):
    Each tool result is rendered into a labelled text block before being returned
    to the LLM. This gives the model a compact, readable summary instead of raw
    JSON blobs — the same approach used by the masmcp parallel_group orchestrator
    (_compose_parallel_query_text / _format_service_output_for_query).

    Format:
        ### <tool_name>
        <extracted text or key=value lines>

    Errors appear as:
        ### <tool_name>
        [ERROR] <message>

    The raw dict result is also preserved in the "result" key for programmatic
    access if the LLM decides to call call_tool again with refined arguments.
"""
from __future__ import annotations

import asyncio
import logging
import os
from typing import Any

from langchain_core.tools import tool

from ..registry_client import call_tool as _call

log = logging.getLogger(__name__)

_MAX_PARALLEL = 10


# ── Result formatting ─────────────────────────────────────────────────────────
# Inspired by masmcp WorkflowBuilder._format_service_output_for_query /
# _compose_parallel_query_text.  Converts raw tool output into compact,
# labelled text blocks that are easy for the LLM to reason over.

def _extract_text_from_result(tool_name: str, value: Any) -> str:
    """Extract a human-readable summary from a single tool result.

    Strategy (in priority order):
    1. Known tool families — specialised extraction.
    2. Flat dict — render as "key: value" lines (skip large nested objects).
    3. List — render first N items.
    4. Scalar — str(value).
    """
    if value is None:
        return ""

    # ── search_knowledge_base / RAG ───────────────────────────────────────────
    if "search_knowledge_base" in tool_name or "rag" in tool_name:
        if isinstance(value, list):
            lines: list[str] = []
            for idx, item in enumerate(value[:5], start=1):
                if isinstance(item, dict):
                    content = item.get("content") or item.get("text") or ""
                    score = item.get("score", "")
                    fname = item.get("filename", "")
                    score_str = f" (score={score})" if score else ""
                    src_str = f" [{fname}]" if fname else ""
                    lines.append(f"[doc{idx}]{score_str}{src_str} {content}")
                else:
                    lines.append(f"[doc{idx}] {item}")
            return "\n".join(lines)

    # ── get_pods / k8s-style list of dicts ───────────────────────────────────
    if isinstance(value, list) and value and isinstance(value[0], dict):
        lines = []
        for idx, item in enumerate(value[:20]):
            # render the most useful fields, skip deeply nested
            parts = []
            for k, v in item.items():
                if isinstance(v, (str, int, float, bool)):
                    parts.append(f"{k}={v}")
            lines.append("  " + "  ".join(parts) if parts else f"  {item}")
        return "\n".join(lines)

    # ── flat dict ─────────────────────────────────────────────────────────────
    if isinstance(value, dict):
        lines = []
        for k, v in value.items():
            if isinstance(v, (str, int, float, bool)):
                lines.append(f"{k}: {v}")
            elif isinstance(v, list):
                lines.append(f"{k}: [{len(v)} items]")
            elif isinstance(v, dict):
                lines.append(f"{k}: {{...}}")
        return "\n".join(lines) if lines else str(value)

    # ── scalar / string ───────────────────────────────────────────────────────
    return str(value)


def _format_parallel_results(raw_results: list[dict]) -> str:
    """Compose all tool results into a single labelled text block for the LLM.

    Each section:
        ### <tool_name>
        <rendered text>

    Errors:
        ### <tool_name>
        [ERROR] <message>
    """
    sections: list[str] = []
    for item in raw_results:
        tool_name = item.get("tool_name", "unknown")
        if "error" in item:
            sections.append(f"### {tool_name}\n[ERROR] {item['error']}")
        else:
            rendered = _extract_text_from_result(tool_name, item.get("result"))
            body = rendered.strip() if rendered.strip() else "(no output)"
            sections.append(f"### {tool_name}\n{body}")
    return "\n\n".join(sections)


@tool
async def call_tools_parallel(calls: list[dict]) -> dict:
    """Call multiple independent tools simultaneously and return all results.

    Use this when you need to gather information from several sources at once
    and the calls do NOT depend on each other's output.

    Example: collecting system state before analysing an incident —
        get_pods, get_metrics, and get_logs can all run at the same time.

    Rules:
    - All calls must be READ-ONLY. Use call_tool + ask_human for any write action.
    - Verify every tool name via list_available_tools first.
    - Max 10 calls per invocation.

    Args:
        calls: list of call descriptors, each with:
               - tool_name (str): exact name from list_available_tools
               - arguments (dict): input matching the tool's input_schema

    Returns:
        summary:  human-readable labelled text blocks — use this to reason over results
        results:  raw list in input order, each with tool_name + result|error keys
    """
    if not calls:
        return {"summary": "[ERROR] calls list is empty", "results": []}

    if len(calls) > _MAX_PARALLEL:
        msg = f"Too many parallel calls: {len(calls)} > {_MAX_PARALLEL}. Split into smaller batches."
        return {"summary": f"[ERROR] {msg}", "results": [{"error": msg}]}

    tenant_id = os.getenv("TENANT_ID", "unknown")
    org_id = os.getenv("ORG_ID", "unknown")

    async def _one(call: dict) -> dict:
        tool_name = call.get("tool_name", "")
        arguments = call.get("arguments", {})
        if not tool_name:
            return {"tool_name": tool_name, "error": "tool_name is required"}
        try:
            result = await _call(
                tool_name=tool_name,
                arguments=arguments,
                tenant_id=tenant_id,
                org_id=org_id,
            )
            if "error" in result:
                return {"tool_name": tool_name, "error": result["error"]}
            return {"tool_name": tool_name, "result": result}
        except Exception as exc:
            log.error("call_tools_parallel: %s failed: %s", tool_name, exc)
            return {"tool_name": tool_name, "error": str(exc)}

    log.info(
        "call_tools_parallel: dispatching %d calls for tenant=%s org=%s",
        len(calls), tenant_id, org_id,
    )
    raw_results: list[dict] = list(await asyncio.gather(*[_one(c) for c in calls]))

    succeeded = sum(1 for r in raw_results if "result" in r)
    log.info("call_tools_parallel: %d/%d calls succeeded", succeeded, len(raw_results))

    return {
        "summary": _format_parallel_results(raw_results),
        "results": raw_results,
    }
