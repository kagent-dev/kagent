"use server";

import { fetchApi, createErrorResponse } from "./utils";
import { getBackendRoot } from "@/lib/utils";
import type { BaseResponse } from "@/types";

export interface PluginItem {
  name: string;
  pathPrefix: string;
  displayName: string;
  icon: string;
  section: string;
}

export type PluginBackendStatus = "ok" | "unreachable" | "not_found" | "checking";

export async function getPlugins(): Promise<BaseResponse<PluginItem[]>> {
  try {
    const response = await fetchApi<BaseResponse<PluginItem[]>>("/plugins");
    if (response.error || !response.data) {
      return {
        data: undefined,
        message: (response as { message?: string }).message ?? "Failed to fetch plugins",
        error: (response as { message?: string }).message,
      };
    }
    return { data: response.data, message: response.message ?? "OK" };
  } catch (err) {
    return createErrorResponse(err, "Failed to fetch plugins");
  }
}

/**
 * Check plugin backend health. Runs on the server so it can reach the
 * in-cluster controller when the UI is accessed via port-forward.
 */
export async function checkPluginBackend(pathPrefix: string): Promise<{
  status: PluginBackendStatus;
  statusCode?: number;
}> {
  const root = getBackendRoot();
  const url = `${root}/_p/${pathPrefix}/`;
  try {
    // Use GET instead of HEAD — some backends (e.g. Temporal UI) reject HEAD with 405.
    const res = await fetch(url, {
      method: "GET",
      cache: "no-store",
      signal: AbortSignal.timeout(5000),
    });
    if (res.status === 200) return { status: "ok", statusCode: res.status };
    if (res.status === 404) return { status: "not_found", statusCode: 404 };
    if (res.status === 502 || res.status === 503) return { status: "unreachable", statusCode: res.status };
    return { status: "unreachable", statusCode: res.status };
  } catch {
    return { status: "unreachable" };
  }
}
