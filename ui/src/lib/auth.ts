import { headers } from "next/headers";
import { NextRequest } from "next/server";

/**
 * Extract authentication headers from a headers-like object.
 * Common implementation used by both server actions and route handlers.
 */
function extractAuthHeaders(getHeader: (name: string) => string | null): Record<string, string> {
  const authHeaders: Record<string, string> = {};

  // Forward Authorization header (JWT token from oauth2-proxy)
  const authHeader = getHeader("Authorization");
  if (authHeader) {
    authHeaders["Authorization"] = authHeader;
  }

  return authHeaders;
}

/**
 * Get authentication headers from incoming request (for route handlers).
 * These are set by oauth2-proxy or other auth proxies.
 */
export function getAuthHeadersFromRequest(request: NextRequest): Record<string, string> {
  return extractAuthHeaders((name) => request.headers.get(name));
}

/**
 * Get authentication headers from request context (for server actions).
 * These are set by oauth2-proxy or other auth proxies.
 */
export async function getAuthHeadersFromContext(): Promise<Record<string, string>> {
  const headersList = await headers();
  return extractAuthHeaders((name) => headersList.get(name));
}
