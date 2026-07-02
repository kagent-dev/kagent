"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { AppRenderer, type AppRendererProps } from "@mcp-ui/client";
import type { CallToolResult, ContentBlock } from "@modelcontextprotocol/sdk/types.js";
import { useTheme } from "next-themes";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useChatMcpApps } from "@/components/chat/ChatMcpAppsContext";
import { callMcpAppTool, readMcpAppResource } from "@/app/actions/mcp-apps";
import { installMcpAppInitCompat } from "@/lib/mcpAppInitCompat";

// Install at module load (before AppRenderer mounts and its transport connects)
// so non-conformant guests that send `clientInfo` instead of `appInfo` in their
// `ui/initialize` request still complete the handshake. See mcpAppInitCompat.
installMcpAppInitCompat();

type SandboxCsp = NonNullable<AppRendererProps["sandbox"]>["csp"];

interface McpAppRendererProps {
  namespace: string;
  serverName: string;
  /**
   * groupKind of the backing tool-server CRD so proxied tool/resource calls
   * resolve the right CRD when a RemoteMCPServer and MCPServer share a
   * namespace/name.
   */
  groupKind?: string;
  toolName: string;
  toolResourceUri: string;
  toolInput?: Record<string, unknown>;
  toolResult?: CallToolResult;
  /**
   * Delivers a message the app pushed into the conversation via the MCP Apps
   * `ui/message` channel. The host injects it as a new chat turn so the agent
   * can act on it (e.g. start monitoring a build after the app triggered it).
   */
  onSendMessage?: (text: string) => Promise<void>;
}

/** Join the text content blocks of an app `ui/message` into a single prompt. */
function extractMessageText(content: ContentBlock[] | undefined): string {
  if (!Array.isArray(content)) {
    return "";
  }
  return content
    .filter((block): block is ContentBlock & { type: "text"; text: string } => block?.type === "text" && typeof (block as { text?: unknown }).text === "string")
    .map((block) => block.text)
    .join("\n")
    .trim();
}

function requireData<T>(response: { data?: T; error?: string; message: string }): T {
  if (response.error || !response.data) {
    throw new Error(response.error || response.message);
  }
  return response.data;
}

export function McpAppRenderer({
  namespace,
  serverName,
  groupKind,
  toolName,
  toolResourceUri,
  toolInput,
  toolResult,
  onSendMessage,
}: McpAppRendererProps) {
  const { resolvedTheme } = useTheme();
  const { getMcpToolForAppCall } = useChatMcpApps();
  const [sandboxUrl, setSandboxUrl] = useState<URL | null>(null);
  const [csp, setCsp] = useState<SandboxCsp>(undefined);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      // ponytail: the proxy is served from the host origin, so with the iframe's
      // allow-same-origin attribute an MCP App shares the host origin (can reach
      // host cookies/storage/DOM/API). Accepted by design: MCP servers are
      // curated/trusted, and a single origin keeps everything behind one SSO
      // gateway. Upgrade path if untrusted servers are ever exposed: serve
      // sandbox_proxy.html from a distinct, env-configured origin so
      // allow-same-origin resolves to the sandbox origin instead of the host.
      // Tell the sandbox proxy which origin to trust for postMessage, so it can
      // reject messages from any other origin instead of accepting "*".
      const url = new URL("/sandbox_proxy.html", window.location.origin);
      url.searchParams.set("parentOrigin", window.location.origin);
      setSandboxUrl(url);
    });
    return () => cancelAnimationFrame(id);
  }, []);

  // Only hand the app its theme/locale once it has connected. @mcp-ui/client@7.1.1
  // fires setHostContext the moment the AppBridge is created — before the iframe
  // transport connects — so providing hostContext earlier throws an unhandled
  // "Not connected" rejection and never reaches the guest. The first
  // size-changed notification only arrives post-connect, so we use it as the
  // connected signal (see handleSizeChanged). ponytail: an app that never
  // reports its size won't get theme sync and falls back to its own theme —
  // acceptable degradation; the upgrade path is a library fix that delivers
  // hostContext at ui/initialize instead of via a pre-connect notification.
  const hostContext = useMemo<AppRendererProps["hostContext"]>(
    () => connected
      ? {
          theme: resolvedTheme === "dark" ? "dark" : "light",
          locale: typeof navigator !== "undefined" ? navigator.language : "en-US",
        }
      : undefined,
    [connected, resolvedTheme],
  );

  // Pass the resource's declared CSP (captured in handleReadResource) to the
  // sandbox proxy so it can enforce it; the proxy applies the spec's restrictive
  // default when this is undefined.
  const sandbox = useMemo(() => sandboxUrl ? { url: sandboxUrl, csp } : undefined, [sandboxUrl, csp]);

  // Advertise the host channels we actually implement so capability-gated apps
  // know they can proxy tool/resource calls and push messages to the chat.
  const hostCapabilities = useMemo<NonNullable<AppRendererProps["hostCapabilities"]>>(() => ({
    serverTools: {},
    serverResources: {},
    openLinks: {},
    message: { text: {} },
  }), []);

  const handleReadResource = useCallback<NonNullable<AppRendererProps["onReadResource"]>>(async ({ uri }) => {
    const result = requireData(await readMcpAppResource(namespace, serverName, uri, groupKind));
    // Capture _meta.ui.csp before the library renders the iframe so the sandbox
    // proxy can enforce the server-declared Content Security Policy.
    const ui = (result.contents?.[0]?._meta as { ui?: { csp?: SandboxCsp } } | undefined)?.ui;
    if (ui?.csp) {
      setCsp(ui.csp);
    }
    return result;
  }, [namespace, serverName, groupKind]);

  // An iframe-initiated tools/call is the app updating itself: proxy it to the
  // MCP server and return the result to the same widget so it re-renders in
  // place (MCP Apps standard). We do NOT promote it to a new chat turn, even
  // when the tool is also model-visible — that would spawn a duplicate widget
  // instead of updating the existing one. Apps that want the agent to act use
  // the separate ui/message channel (onMessage) instead.
  const handleCallTool = useCallback<NonNullable<AppRendererProps["onCallTool"]>>(async (params) => {
    const requestedToolName = typeof params.name === "string" && params.name ? params.name : toolName;
    const args = typeof params.arguments === "object" && params.arguments !== null
      ? (params.arguments as Record<string, unknown>)
      : {};
    const requestedTool = getMcpToolForAppCall(namespace, serverName, requestedToolName);

    if (requestedTool && !requestedTool.appOnly && !requestedTool.agentVisible) {
      throw new Error(`MCP App requested tool ${requestedToolName}, but it is not configured as app-only or agent-visible.`);
    }

    return requireData(await callMcpAppTool(namespace, serverName, requestedToolName, args, groupKind));
  }, [getMcpToolForAppCall, namespace, serverName, toolName, groupKind]);

  // ui/message: the app asks the host to inject content into the conversation.
  // We forward it as a new chat turn so the agent can react (e.g. start
  // monitoring a build the app just triggered).
  const handleMessage = useCallback<NonNullable<AppRendererProps["onMessage"]>>(async (params) => {
    const text = extractMessageText(params.content as ContentBlock[] | undefined);
    if (!text) {
      return { isError: true };
    }
    if (!onSendMessage) {
      return { isError: true };
    }
    try {
      await onSendMessage(text);
      return {};
    } catch {
      return { isError: true };
    }
  }, [onSendMessage]);

  // Gracefully accept protocol requests we don't act on (e.g.
  // ui/update-model-context) so apps that send them don't surface errors.
  const handleFallbackRequest = useCallback<NonNullable<AppRendererProps["onFallbackRequest"]>>(async () => {
    return {};
  }, []);

  const handleOpenLink = useCallback<NonNullable<AppRendererProps["onOpenLink"]>>(async ({ url }) => {
    const target = new URL(String(url), window.location.href);
    if (target.protocol !== "http:" && target.protocol !== "https:") {
      const message = `Blocked unsupported link scheme: ${target.protocol}`;
      setError(message);
      throw new Error(message);
    }
    window.open(target.toString(), "_blank", "noopener,noreferrer");
    return {};
  }, []);

  const handleError = useCallback<NonNullable<AppRendererProps["onError"]>>((err) => {
    setError(err.message);
  }, []);

  // The guest can only emit size-changed once its transport is connected, so we
  // treat the first one as the signal that it's safe to push hostContext.
  const handleSizeChanged = useCallback<NonNullable<AppRendererProps["onSizeChanged"]>>(() => {
    setConnected(true);
  }, []);

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>MCP App error</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  if (!sandboxUrl) {
    return <div className="rounded-md border p-4 text-sm text-muted-foreground">Preparing MCP App renderer...</div>;
  }

  return (
    <div className="isolate w-full overflow-x-auto overflow-y-visible overscroll-x-contain rounded-lg border bg-background [&_iframe]:w-full [&_iframe]:min-w-[760px]">
      <AppRenderer
        toolName={toolName}
        toolResourceUri={toolResourceUri}
        toolInput={toolInput}
        toolResult={toolResult}
        sandbox={sandbox as NonNullable<AppRendererProps["sandbox"]>}
        hostContext={hostContext}
        hostCapabilities={hostCapabilities}
        onReadResource={handleReadResource}
        onCallTool={handleCallTool}
        onMessage={handleMessage}
        onFallbackRequest={handleFallbackRequest}
        onOpenLink={handleOpenLink}
        onSizeChanged={handleSizeChanged}
        onError={handleError}
      />
    </div>
  );
}
