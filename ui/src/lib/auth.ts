import { headers } from "next/headers";
import { NextRequest } from "next/server";

// Headers that must never be forwarded to the backend, regardless of
// KAGENT_ADDITIONAL_FORWARDED_HEADERS configuration. These are hop-by-hop
// (RFC 7230 §6.1) or determined by the upstream fetch.
const BLOCKED_FORWARD_HEADERS = new Set([
  "host",
  "connection",
  "keep-alive",
  "transfer-encoding",
  "upgrade",
  "te",
  "trailer",
  "proxy-authenticate",
  "proxy-authorization",
  "content-length",
  "content-encoding",
]);

// Headers always forwarded. The Authorization header carries the JWT from
// oauth2-proxy and is required for the backend to identify the user.
const DEFAULT_FORWARD_HEADERS = ["authorization"] as const;

/**
 * Parse a comma-separated header allowlist (case-insensitive) and union it
 * with the default forward headers. Hop-by-hop / routing headers are dropped
 * and returned separately so callers can surface a warning.
 *
 * Pure function — no env or module state. Exported for unit testing.
 */
export function parseAllowedForwardHeaders(raw: string | undefined): {
  allowed: Set<string>;
  blocked: string[];
} {
  const allowed = new Set<string>(DEFAULT_FORWARD_HEADERS);
  const blocked: string[] = [];
  if (!raw) {
    return { allowed, blocked };
  }
  for (const part of raw.split(",")) {
    const name = part.trim().toLowerCase();
    if (!name) continue;
    if (BLOCKED_FORWARD_HEADERS.has(name)) {
      blocked.push(name);
      continue;
    }
    allowed.add(name);
  }
  return { allowed, blocked };
}

let warnedBlocked = false;

function getAllowedHeadersFromEnv(): Set<string> {
  const { allowed, blocked } = parseAllowedForwardHeaders(
    process.env.KAGENT_ADDITIONAL_FORWARDED_HEADERS
  );
  if (blocked.length > 0 && !warnedBlocked) {
    warnedBlocked = true;
    console.warn(
      `KAGENT_ADDITIONAL_FORWARDED_HEADERS contains hop-by-hop or routing headers that will not be forwarded: ${blocked.join(", ")}`
    );
  }
  return allowed;
}

/**
 * CORS Access-Control-Allow-Headers value for proxy routes.
 * Kept intentionally minimal and decoupled from KAGENT_ADDITIONAL_FORWARDED_HEADERS:
 * forwarded identity-style headers (e.g. x-auth-request-user) must not be
 * accepted from cross-origin browsers, even when the backend trusts them
 * server-to-server.
 */
export const CORS_ALLOW_HEADERS = "Content-Type, Authorization, Accept";

/**
 * Copy headers named in `allowed` from `getHeader` into a forwardable record.
 * Pure function — exported for unit testing.
 */
export function extractAllowedHeaders(
  allowed: Set<string>,
  getHeader: (name: string) => string | null
): Record<string, string> {
  const forwarded: Record<string, string> = {};
  for (const name of allowed) {
    const value = getHeader(name);
    if (value) {
      forwarded[name] = value;
    }
  }
  return forwarded;
}

function extractAuthHeaders(getHeader: (name: string) => string | null): Record<string, string> {
  return extractAllowedHeaders(getAllowedHeadersFromEnv(), getHeader);
}

/**
 * Get authentication and additional forwarded headers from incoming request (for route handlers).
 * Always forwards Authorization (set by oauth2-proxy or other auth proxies); extra headers
 * may be configured via KAGENT_ADDITIONAL_FORWARDED_HEADERS.
 */
export function getAuthHeadersFromRequest(request: NextRequest): Record<string, string> {
  return extractAuthHeaders((name) => request.headers.get(name));
}

/**
 * Get authentication and additional forwarded headers from request context (for server actions).
 * Always forwards Authorization (set by oauth2-proxy or other auth proxies); extra headers
 * may be configured via KAGENT_ADDITIONAL_FORWARDED_HEADERS.
 */
export async function getAuthHeadersFromContext(): Promise<Record<string, string>> {
  const headersList = await headers();
  return extractAuthHeaders((name) => headersList.get(name));
}
