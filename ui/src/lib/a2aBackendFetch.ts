import type { NextRequest } from "next/server";
import { getBackendUrl } from "@/lib/utils";
import { getCurrentUserId } from "@/app/actions/utils";

/**
 * Server-side fetch to kagent /api/a2a/... Must mirror {@link fetchApi}: same user_id
 * query param and forwarded auth so DB session lookups match session creation.
 */
export async function fetchA2ABackend(
  request: NextRequest,
  pathAfterApi: string,
  body: unknown
): Promise<Response> {
  const base = getBackendUrl().replace(/\/$/, "");
  const path = pathAfterApi.startsWith("/") ? pathAfterApi : `/${pathAfterApi}`;
  const url = new URL(`${base}${path}`);
  const userId = await getCurrentUserId();
  if (!url.searchParams.has("user_id")) {
    url.searchParams.set("user_id", userId);
  }

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Accept: "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
    "User-Agent": "kagent-ui",
  };
  const auth = request.headers.get("authorization");
  if (auth) headers.Authorization = auth;
  const cookie = request.headers.get("cookie");
  if (cookie) headers.Cookie = cookie;
  const xUser = request.headers.get("x-user-id");
  if (xUser) headers["X-User-Id"] = xUser;

  return fetch(url.toString(), {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
}
