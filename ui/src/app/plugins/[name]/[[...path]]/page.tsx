"use client";

import { useParams } from "next/navigation";
import { useEffect, useRef, useState, useCallback } from "react";
import { useTheme } from "next-themes";
import { useNamespace } from "@/lib/namespace-context";

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

  // Build iframe src - Go backend reverse proxies to plugin service
  const pathParams = useParams<{ path?: string[] }>();
  const subPath = pathParams.path ? "/" + pathParams.path.join("/") : "/";
  const iframeSrc = `/plugins/${name}${subPath}`;

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
      <iframe
        ref={iframeRef}
        src={iframeSrc}
        className="flex-1 border-0"
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        title={`Plugin: ${name}`}
      />
    </div>
  );
}
