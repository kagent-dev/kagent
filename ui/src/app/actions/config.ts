"use server";

import { DEFAULT_STREAM_TIMEOUT_MS } from "@/lib/constants";

export interface UiRuntimeConfig {
  streamTimeoutMs: number;
}

/**
 * Returns runtime UI configuration sourced from server-side environment
 * variables (set by the Helm chart). Read on the server so values reflect the
 * deployment at runtime, unlike NEXT_PUBLIC_* vars which are inlined at build.
 */
export async function getUiRuntimeConfig(): Promise<UiRuntimeConfig> {
  const raw = process.env.KAGENT_STREAM_TIMEOUT_MS;
  const parsed = raw ? Number(raw) : NaN;
  const streamTimeoutMs = Number.isFinite(parsed) && parsed > 0 ? parsed : DEFAULT_STREAM_TIMEOUT_MS;
  return { streamTimeoutMs };
}
