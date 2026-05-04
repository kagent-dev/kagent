"use client";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useCallback, useEffect, useRef, useState } from "react";

/** Browser-reachable HTTP API base for WebSocket upgrade (same-origin /api when deployed behind nginx). */
function terminalApiBase(): string {
  const envOnly = process.env.NEXT_PUBLIC_SANDBOX_SSH_HTTP_BASE?.trim();
  if (envOnly) {
    return envOnly.replace(/\/+$/, "");
  }
  const backend = process.env.NEXT_PUBLIC_BACKEND_URL?.trim() ?? "";
  if (backend.startsWith("/")) {
    return new URL(backend, window.location.origin).href.replace(/\/+$/, "");
  }
  if (
    (backend.startsWith("http://") || backend.startsWith("https://")) &&
    !backend.includes(".svc.cluster.local")
  ) {
    return backend.replace(/\/+$/, "");
  }
  return new URL("/api", window.location.origin).href.replace(/\/+$/, "");
}

function sandboxSshWebSocketURL(apiBase: string): string {
  const u = new URL(apiBase);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  const basePath = u.pathname.replace(/\/?$/, "");
  u.pathname = `${basePath}/sandbox/ssh`;
  return u.toString();
}

export function NemoclawPlayground() {
  const [sandboxName, setSandboxName] = useState("openclaw-openai");
  const [plainShell, setPlainShell] = useState(false);
  const [termError, setTermError] = useState<string | null>(null);
  const [sessionActive, setSessionActive] = useState(false);
  const [connecting, setConnecting] = useState(false);

  const termHostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const el = termHostRef.current;
    if (!el) return;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      theme: {
        background: "#0c0c0c",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    fit.fit();
    termRef.current = term;
    fitRef.current = fit;

    const ro = new ResizeObserver(() => {
      fit.fit();
      const ws = wsRef.current;
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({
            type: "resize",
            cols: term.cols,
            rows: term.rows,
          })
        );
      }
    });
    ro.observe(el);

    term.onData((data) => {
      const ws = wsRef.current;
      if (ws?.readyState === WebSocket.OPEN) ws.send(data);
    });

    return () => {
      ro.disconnect();
      wsRef.current?.close();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, []);

  const onDisconnect = useCallback(() => {
    wsRef.current?.close();
  }, []);

  const onConnectTerminal = useCallback(() => {
    const term = termRef.current;
    if (!term) return;
    const name = sandboxName.trim();
    if (!name) {
      setTermError("Sandbox name is required.");
      return;
    }

    setTermError(null);
    setConnecting(true);
    setSessionActive(false);
    wsRef.current?.close();

    const url = sandboxSshWebSocketURL(terminalApiBase());
    let ws: WebSocket;
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setConnecting(false);
      setTermError(e instanceof Error ? e.message : String(e));
      return;
    }
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      if (wsRef.current !== ws) return;
      setConnecting(false);
      setSessionActive(true);
      setTermError(null);
      term.reset();
      ws.send(
        JSON.stringify({
          sandbox_name: name,
          plain_shell: plainShell,
          cols: term.cols,
          rows: term.rows,
        })
      );
    };

    ws.onmessage = (ev) => {
      if (wsRef.current !== ws) return;
      if (typeof ev.data === "string") {
        try {
          const msg = JSON.parse(ev.data) as { type?: string; message?: string };
          if (msg.type === "error") {
            term.writeln(`\r\n\x1b[31m${msg.message ?? "error"}\x1b[0m`);
            return;
          }
          if (msg.type === "ready") {
            return;
          }
        } catch {
          term.write(ev.data);
        }
        return;
      }
      term.write(new Uint8Array(ev.data as ArrayBuffer));
    };

    ws.onerror = () => {
      if (wsRef.current !== ws) return;
      setTermError("WebSocket error — check DevTools → Network → WS and that /api reaches the controller.");
    };

    ws.onclose = (ev) => {
      if (wsRef.current !== ws) return;
      wsRef.current = null;
      setConnecting(false);
      setSessionActive(false);
      term.writeln(`\r\n\x1b[90m(disconnected)\x1b[0m`);
      if (!ev.wasClean && ev.code === 1006) {
        setTermError("WebSocket closed abnormally (1006). Check nginx /api proxy and controller logs.");
      }
    };
  }, [sandboxName, plainShell]);

  return (
    <div className="max-w-7xl mx-auto px-4 py-8 space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Sandbox terminal</h1>
        <p className="text-muted-foreground mt-2 text-sm">
          Sandboxes are expected to run in the <span className="font-medium text-foreground">openshell</span> namespace (OpenShell gRPC
          defaults match that layout).
        </p>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Connect</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:gap-6">
            <div className="grid gap-2 flex-1 max-w-md">
              <Label htmlFor="sandbox">Sandbox name</Label>
              <Input
                id="sandbox"
                value={sandboxName}
                onChange={(e) => setSandboxName(e.target.value)}
                placeholder="openclaw-openai"
                autoComplete="off"
                disabled={sessionActive || connecting}
              />
            </div>
            <div className="flex items-center gap-2 pb-2">
              <Checkbox
                id="plain-shell"
                checked={plainShell}
                onCheckedChange={(v) => setPlainShell(v === true)}
                disabled={sessionActive || connecting}
              />
              <Label htmlFor="plain-shell" className="text-sm font-normal cursor-pointer leading-snug">
                Plain shell only (skip <code className="text-xs bg-muted px-1 rounded">openclaw tui</code>)
              </Label>
            </div>
            <Button
              type="button"
              variant={sessionActive ? "secondary" : connecting ? "outline" : "default"}
              onClick={sessionActive || connecting ? onDisconnect : onConnectTerminal}
              className="sm:mb-0.5"
            >
              {connecting ? "Cancel" : sessionActive ? "Disconnect" : "Connect"}
            </Button>
          </div>
          {termError ? <p className="text-sm text-destructive">{termError}</p> : null}
          <div ref={termHostRef} className="w-full min-h-[480px] h-[52vh] border rounded-md overflow-hidden bg-[#0c0c0c]" />
        </CardContent>
      </Card>
    </div>
  );
}
