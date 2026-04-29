"use client";

import { Suspense, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { ArrowLeft, Loader2 } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { McpServerForm } from "@/components/mcp/McpServerForm";
import { createServer, getToolServerTypes } from "@/app/actions/servers";
import { useAgents } from "@/components/AgentsProvider";
import type { ToolServerCreateRequest } from "@/types";
import { ErrorState } from "@/components/ErrorState";

function NewMcpServerContent() {
  const router = useRouter();
  const { refreshTools } = useAgents();
  const [types, setTypes] = useState<string[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const loadTypes = useCallback(async () => {
    setLoadError(null);
    const r = await getToolServerTypes();
    if (r.data && !r.error) {
      setTypes(r.data);
      return;
    }
    setLoadError(r.error || "Failed to load supported tool server types");
  }, []);

  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      void loadTypes();
    });
    return () => cancelAnimationFrame(raf);
  }, [loadTypes]);

  const onCreate = useCallback(
    async (req: ToolServerCreateRequest) => {
      const r = await createServer(req);
      if (r.error) {
        throw new Error(r.error);
      }
      await refreshTools();
      toast.success("MCP server created");
      router.push("/mcp");
    },
    [refreshTools, router],
  );

  if (loadError) {
    return <ErrorState message={loadError} />;
  }

  if (types === null) {
    return (
      <AppPageFrame mainClassName="mx-auto max-w-3xl px-4 py-20 sm:px-6">
        <div className="flex items-center justify-center gap-2 text-sm text-muted-foreground" role="status" aria-live="polite">
          <Loader2 className="h-5 w-5 animate-spin" aria-hidden />
          Loading form…
        </div>
      </AppPageFrame>
    );
  }

  return (
    <AppPageFrame ariaLabelledBy="mcp-new-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <div>
        <Link
          href="/mcp"
          className="mb-8 inline-flex items-center gap-2 rounded-sm text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to MCP & tools
        </Link>

        <PageHeader titleId="mcp-new-title" title="New MCP server" className="mb-8" />

        <McpServerForm supportedToolServerTypes={types} onCreate={onCreate} />
      </div>
    </AppPageFrame>
  );
}

export default function NewMcpServerPage() {
  return (
    <Suspense
      fallback={
        <AppPageFrame mainClassName="mx-auto max-w-3xl px-4 py-20 sm:px-6">
          <div className="flex justify-center text-sm text-muted-foreground" role="status" aria-live="polite">
            <Loader2 className="h-5 w-5 animate-spin" aria-hidden />
            Loading…
          </div>
        </AppPageFrame>
      }
    >
      <NewMcpServerContent />
    </Suspense>
  );
}
