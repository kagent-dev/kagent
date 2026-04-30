"use client";

import { useCallback, useEffect, useState } from "react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { McpServersView } from "@/components/mcp/McpServersView";
import { getServers } from "@/app/actions/servers";
import type { ToolServerResponse } from "@/types";
export default function McpPage() {
  const [servers, setServers] = useState<ToolServerResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    const rServers = await getServers();
    if (rServers.error || !rServers.data) {
      setLoadError(rServers.error || "Failed to load MCP data");
      setServers([]);
    } else {
      const sorted = [...rServers.data].sort((a, b) => (a.ref || "").localeCompare(b.ref || ""));
      setServers(sorted);
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      void load();
    });
    return () => cancelAnimationFrame(raf);
  }, [load]);

  return (
    <AppPageFrame ariaLabelledBy="mcp-page-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <PageHeader
        titleId="mcp-page-title"
        title="MCP & tools"
        description="Add MCP servers to your cluster, then search or expand each server to see the tools agents can use."
        className="mb-6"
      />

      <McpServersView servers={servers} isLoading={loading} loadError={loadError} onRefresh={load} />
    </AppPageFrame>
  );
}
