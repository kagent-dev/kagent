"use client";

import { Suspense } from "react";
import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { ArrowLeft } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { McpAppsInspector } from "@/components/mcp-apps/McpAppsInspector";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

function SingleMcpAppContent() {
  const params = useParams<{ appName: string }>();
  const searchParams = useSearchParams();

  const appName = params?.appName ? decodeURIComponent(params.appName) : "";
  const namespace = searchParams.get("ns") || "";
  const server = searchParams.get("server") || "";

  return (
    <AppPageFrame ariaLabelledBy="mcp-app-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <Link
        href="/mcp"
        className="mb-6 inline-flex items-center gap-2 rounded-sm text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden />
        Back to MCP & tools
      </Link>

      <h1 id="mcp-app-title" className="sr-only">
        MCP App {appName}
      </h1>

      {!namespace || !server || !appName ? (
        <Alert variant="destructive">
          <AlertTitle>Missing app context</AlertTitle>
          <AlertDescription>
            This page needs a server reference. Open an MCP App from the MCP &amp; tools list.
          </AlertDescription>
        </Alert>
      ) : (
        <McpAppsInspector namespace={namespace} name={server} focusToolName={appName} />
      )}
    </AppPageFrame>
  );
}

export default function McpAppPage() {
  return (
    <Suspense fallback={null}>
      <SingleMcpAppContent />
    </Suspense>
  );
}
