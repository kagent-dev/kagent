import { NextRequest, NextResponse } from "next/server";
import { getBackendUrl } from "@/lib/utils";
import { CORS_ALLOW_HEADERS, getAuthHeadersFromRequest } from "@/lib/auth";

export function mcpAppsBackendPath(namespace: string, name: string): string {
  return `/mcp-apps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;
}

export async function proxyMcpAppsRequest(
  request: NextRequest,
  path: string,
  init: RequestInit = {},
) {
  try {
    const authHeaders = getAuthHeadersFromRequest(request);
    const response = await fetch(`${getBackendUrl()}${path}`, {
      ...init,
      cache: "no-store",
      headers: {
        ...authHeaders,
        Accept: "application/json",
        "Content-Type": "application/json",
        ...init.headers,
      },
      signal: AbortSignal.timeout(120000),
    });

    const text = await response.text();
    return new Response(text, {
      status: response.status,
      headers: {
        "Content-Type": response.headers.get("content-type") || "application/json",
        "Cache-Control": "no-store",
      },
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : "MCP Apps proxy request failed";
    return NextResponse.json({ error: message, message }, { status: 503 });
  }
}

export function optionsResponse() {
  return new Response(null, {
    status: 200,
    headers: {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
      "Access-Control-Allow-Headers": CORS_ALLOW_HEADERS,
      "Access-Control-Max-Age": "86400",
    },
  });
}
