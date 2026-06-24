"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { SubstrateStatusView } from "@/components/substrate/SubstrateStatusView";
import { getSubstrateStatus } from "@/app/actions/substrate";
import type { SubstrateStatusResponse } from "@/types";

/** Default auto-refresh interval for substrate status (ms). Override via prop or NEXT_PUBLIC_SUBSTRATE_REFRESH_INTERVAL_MS. */
export const DEFAULT_SUBSTRATE_REFRESH_INTERVAL_MS = 2000;

const SUBSTRATE_REFRESH_INTERVAL_KEY = "kagent-substrate-refresh-interval-ms";

type SubstrateStatusPageProps = {
  /** Initial auto-refresh interval in ms when the user has no saved preference. Set to 0 to disable polling. */
  defaultRefreshIntervalMs?: number;
};

function resolveDefaultRefreshIntervalMs(prop?: number): number {
  if (prop !== undefined) return prop;
  const env = process.env.NEXT_PUBLIC_SUBSTRATE_REFRESH_INTERVAL_MS;
  if (env !== undefined && env !== "") {
    const parsed = Number(env);
    if (Number.isFinite(parsed) && parsed >= 0) return parsed;
  }
  return DEFAULT_SUBSTRATE_REFRESH_INTERVAL_MS;
}

function readStoredRefreshIntervalMs(fallbackMs: number): number {
  if (typeof window === "undefined") {
    return fallbackMs;
  }
  try {
    const stored = window.localStorage.getItem(SUBSTRATE_REFRESH_INTERVAL_KEY);
    if (!stored) return fallbackMs;
    const parsed = Number(stored);
    if (Number.isFinite(parsed) && parsed >= 0) return parsed;
  } catch {
    // ignore private mode / quota
  }
  return fallbackMs;
}

export function SubstrateStatusPage({ defaultRefreshIntervalMs: defaultRefreshIntervalMsProp }: SubstrateStatusPageProps = {}) {
  const resolvedDefaultMs = resolveDefaultRefreshIntervalMs(defaultRefreshIntervalMsProp);
  const [refreshIntervalMs, setRefreshIntervalMs] = useState(resolvedDefaultMs);

  const router = useRouter();
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";

  const [status, setStatus] = useState<SubstrateStatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const latestRequestId = useRef(0);

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      setRefreshIntervalMs(readStoredRefreshIntervalMs(resolvedDefaultMs));
    });
    return () => cancelAnimationFrame(id);
  }, [resolvedDefaultMs]);

  const handleRefreshIntervalChange = useCallback((intervalMs: number) => {
    setRefreshIntervalMs(intervalMs);
    try {
      window.localStorage.setItem(SUBSTRATE_REFRESH_INTERVAL_KEY, String(intervalMs));
    } catch {
      // ignore private mode / quota
    }
  }, []);

  const load = useCallback(
    async (opts?: { silent?: boolean }) => {
      const silent = opts?.silent ?? false;
      const requestId = latestRequestId.current + 1;
      latestRequestId.current = requestId;

      if (!silent) {
        setLoading(true);
        setLoadError(null);
      }

      const result = await getSubstrateStatus(namespace || undefined);

      if (requestId !== latestRequestId.current) {
        return;
      }

      if (result.error || !result.data) {
        if (!silent) {
          setLoadError(result.error || "Failed to load substrate status");
          setStatus(null);
        }
      } else {
        setStatus(result.data);
        if (!silent) {
          setLoadError(null);
        }
      }

      if (!silent) {
        setLoading(false);
      }
    },
    [namespace],
  );

  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      void load({ silent: false });
    });
    return () => cancelAnimationFrame(raf);
  }, [load]);

  useEffect(() => {
    if (refreshIntervalMs <= 0) return;

    let cancelled = false;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;

    const schedulePoll = () => {
      if (cancelled) return;
      timeoutId = setTimeout(() => {
        void load({ silent: true }).finally(() => {
          schedulePoll();
        });
      }, refreshIntervalMs);
    };

    schedulePoll();

    return () => {
      cancelled = true;
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
    };
  }, [load, refreshIntervalMs]);

  const handleNamespaceChange = useCallback(
    (ns: string) => {
      const params = new URLSearchParams(searchParams.toString());
      if (ns) {
        params.set("namespace", ns);
      } else {
        params.delete("namespace");
      }
      const q = params.toString();
      router.replace(q ? `/substrate?${q}` : "/substrate");
    },
    [router, searchParams],
  );

  const handleRefresh = useCallback(() => load({ silent: false }), [load]);

  return (
    <AppPageFrame ariaLabelledBy="substrate-page-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <PageHeader
        titleId="substrate-page-title"
        title="Agent Substrate"
        description="Worker pools and actor templates from Kubernetes, plus live actors and worker assignments from ate-api."
        className="mb-8"
      />
      <SubstrateStatusView
        status={status}
        namespace={namespace}
        onNamespaceChange={handleNamespaceChange}
        isLoading={loading}
        loadError={loadError}
        onRefresh={handleRefresh}
        refreshIntervalMs={refreshIntervalMs}
        onRefreshIntervalChange={handleRefreshIntervalChange}
      />
    </AppPageFrame>
  );
}
