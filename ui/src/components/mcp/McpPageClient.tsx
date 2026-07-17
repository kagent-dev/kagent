"use client";

import { useCallback, useState } from "react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { McpServersView } from "@/components/mcp/McpServersView";
import { getServers } from "@/app/actions/servers";
import type { ToolServerResponse } from "@/types";

const sortServers = (servers: ToolServerResponse[]) =>
  [...servers].sort((a, b) => (a.ref || "").localeCompare(b.ref || ""));

export function McpPageClient({
  initialServers,
  initialError,
}: {
  initialServers: ToolServerResponse[];
  initialError: string | null;
}) {
  const [servers, setServers] = useState(() => sortServers(initialServers));
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(initialError);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    const response = await getServers();
    if (response.error || !response.data) {
      setLoadError(response.error || "Failed to load MCP data");
      setServers([]);
    } else {
      setServers(sortServers(response.data));
    }
    setLoading(false);
  }, []);

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
