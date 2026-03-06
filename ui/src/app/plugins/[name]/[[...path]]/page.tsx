"use client";

import { useParams } from "next/navigation";
import { useEffect, useRef, useState, useCallback } from "react";
import { useTheme } from "next-themes";
import { useNamespace } from "@/lib/namespace-context";
import { AlertCircle, Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";

interface PluginMessage {
  type: string;
  payload: unknown;
}

export default function PluginPage() {
  const { name } = useParams<{ name: string }>();
  const { resolvedTheme } = useTheme();
  const { namespace } = useNamespace();
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [title, setTitle] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [retryKey, setRetryKey] = useState(0);

  // Build iframe src using /_p/ prefix - Go backend reverse proxies to plugin service
  // Browser URL /plugins/{name} stays on Next.js; iframe loads from /_p/{name}/ via Go proxy
  const pathParams = useParams<{ path?: string[] }>();
  const subPath = pathParams.path ? "/" + pathParams.path.join("/") : "/";
  const iframeSrc = `/_p/${name}${subPath}`;

  const handleRetry = useCallback(() => {
    setLoading(true);
    setError(false);
    setRetryKey((k) => k + 1);
  }, []);

  // Attach load/error handlers directly on the iframe element.
  // React's synthetic event system doesn't support onError for iframes.
  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;
    const onLoad = () => { setLoading(false); setError(false); };
    const onError = () => { setLoading(false); setError(true); };
    iframe.addEventListener("load", onLoad);
    iframe.addEventListener("error", onError);
    return () => {
      iframe.removeEventListener("load", onLoad);
      iframe.removeEventListener("error", onError);
    };
  }, [retryKey]);

  const sendContext = useCallback(() => {
    const iframe = iframeRef.current;
    if (!iframe?.contentWindow) return;

    const msg: PluginMessage = {
      type: "kagent:context",
      payload: {
        theme: resolvedTheme,
        namespace,
        authToken: null,
      },
    };
    iframe.contentWindow.postMessage(msg, "*");
  }, [resolvedTheme, namespace]);

  // Send context to iframe on changes
  useEffect(() => {
    sendContext();
  }, [sendContext]);

  // Listen for messages from iframe
  useEffect(() => {
    const handler = (event: MessageEvent<PluginMessage>) => {
      if (!event.data?.type?.startsWith("kagent:")) return;

      switch (event.data.type) {
        case "kagent:navigate": {
          const { href } = event.data.payload as { href: string };
          window.location.href = href;
          break;
        }
        case "kagent:resize": {
          const { height } = event.data.payload as { height: number };
          if (iframeRef.current && height > 0) {
            iframeRef.current.style.height = `${height}px`;
          }
          break;
        }
        case "kagent:badge": {
          const badge = event.data.payload as { count?: number; label?: string };
          window.dispatchEvent(
            new CustomEvent("kagent:plugin-badge", {
              detail: { plugin: name, ...badge },
            })
          );
          break;
        }
        case "kagent:title": {
          const { title: newTitle } = event.data.payload as { title: string };
          setTitle(newTitle);
          break;
        }
        case "kagent:ready": {
          sendContext();
          break;
        }
      }
    };

    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, [name, sendContext]);

  return (
    <div className="flex h-full flex-col">
      {title && (
        <div className="flex h-10 items-center border-b px-3">
          <h1 className="text-sm font-semibold">{title}</h1>
        </div>
      )}

      {loading && !error && (
        <div data-testid="plugin-loading" className="flex flex-1 flex-col items-center justify-center gap-3">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          <p className="text-sm text-muted-foreground">Loading plugin…</p>
        </div>
      )}

      {error && (
        <div data-testid="plugin-error" className="flex flex-1 flex-col items-center justify-center gap-4">
          <AlertCircle className="h-10 w-10 text-destructive" />
          <div className="text-center">
            <p className="font-medium">Plugin unavailable</p>
            <p className="text-sm text-muted-foreground mt-1">
              Could not load <span className="font-mono">{name}</span>. The plugin service may be down.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={handleRetry}>
            <RefreshCw className="h-4 w-4 mr-2" />
            Retry
          </Button>
        </div>
      )}

      <iframe
        key={retryKey}
        ref={iframeRef}
        src={iframeSrc}
        className={`flex-1 border-0 ${loading || error ? "hidden" : ""}`}
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        title={`Plugin: ${name}`}
      />
    </div>
  );
}
