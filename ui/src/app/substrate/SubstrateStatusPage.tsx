"use client";

import { useCallback, useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { SubstrateStatusView } from "@/components/substrate/SubstrateStatusView";
import { getSubstrateStatus } from "@/app/actions/substrate";
import type { SubstrateStatusResponse } from "@/types";

export function SubstrateStatusPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";

  const [status, setStatus] = useState<SubstrateStatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    const result = await getSubstrateStatus(namespace || undefined);
    if (result.error || !result.data) {
      setLoadError(result.error || "Failed to load substrate status");
      setStatus(null);
    } else {
      setStatus(result.data);
    }
    setLoading(false);
  }, [namespace]);

  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      void load();
    });
    return () => cancelAnimationFrame(raf);
  }, [load]);

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
        onRefresh={load}
      />
    </AppPageFrame>
  );
}
