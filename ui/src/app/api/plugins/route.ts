import { NextResponse } from "next/server";
import { getBackendUrl } from "@/lib/utils";

/**
 * GET /api/plugins — proxy to backend so client components (e.g. AppSidebarNav)
 * can load the plugin list. Runs on the server and uses in-cluster backend URL
 * when deployed.
 */
export async function GET() {
  try {
    const base = getBackendUrl();
    const url = `${base}/plugins?user_id=admin@kagent.dev`;
    const res = await fetch(url, {
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal: AbortSignal.timeout(10000),
    });
    const json = await res.json().catch(() => ({}));
    if (!res.ok) {
      return NextResponse.json(
        { data: [], message: json.message ?? `HTTP ${res.status}` },
        { status: res.status }
      );
    }
    return NextResponse.json(json);
  } catch (err) {
    const message = err instanceof Error ? err.message : "Failed to load plugins";
    return NextResponse.json(
      { data: [], message },
      { status: 503 }
    );
  }
}
