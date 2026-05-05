"use client";

import { Button } from "@/components/ui/button";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

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

export function OpenshellTerminalPage() {
  const searchParams = useSearchParams();

  const gatewaySandboxName = searchParams.get("sandbox")?.trim() ?? "";
  const autoConnect = searchParams.get("connect") === "1";
  const namespace = searchParams.get("ns")?.trim() ?? "";
  const crName = searchParams.get("name")?.trim() ?? "";
  const modelConfigRef = searchParams.get("modelConfigRef")?.trim() ?? "";
  const plainShell = searchParams.get("plainShell") === "1";

  const displayTitle =
    namespace && crName ? `${namespace}/${crName}` : gatewaySandboxName || "OpenShell";

  const [termError, setTermError] = useState<string | null>(null);
  const [sessionActive, setSessionActive] = useState(false);
  const [connecting, setConnecting] = useState(() => Boolean(autoConnect && gatewaySandboxName));

  const termHostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
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

    const ro = new ResizeObserver(() => {
      fit.fit();
      const ws = wsRef.current;
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({
            type: "resize",
            cols: term.cols,
            rows: term.rows,
          }),
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
    };
  }, []);

  const onDisconnect = useCallback(() => {
    wsRef.current?.close();
  }, []);

  const connectTerminal = useCallback(
    (gatewayName: string) => {
      const term = termRef.current;
      if (!term) {
        setConnecting(false);
        return;
      }
      const name = gatewayName.trim();
      if (!name) {
        setTermError("Missing gateway sandbox name.");
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
          }),
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
        setTermError("WebSocket error — check Network → WS and that /api reaches the controller.");
      };

      ws.onclose = (ev) => {
        if (wsRef.current !== ws) return;
        wsRef.current = null;
        setConnecting(false);
        setSessionActive(false);
        term.writeln(`\r\n\x1b[90m(disconnected)\x1b[0m`);
        if (!ev.wasClean && ev.code === 1006) {
          setTermError("Connection closed abnormally (1006).");
        }
      };
    },
    [plainShell],
  );

  useEffect(() => {
    if (!autoConnect || !gatewaySandboxName) return;
    const t = window.setTimeout(() => {
      if (!termRef.current) return;
      connectTerminal(gatewaySandboxName);
    }, 400);
    return () => window.clearTimeout(t);
  }, [autoConnect, gatewaySandboxName, connectTerminal]);

  const showConnectButton = Boolean(gatewaySandboxName) && !sessionActive && !connecting;

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4 px-4 py-6">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border/60 pb-4">
        <div className="min-w-0 space-y-1">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">OpenShell</p>
          <h1 className="truncate text-xl font-semibold tracking-tight text-foreground">{displayTitle}</h1>
          <dl className="flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
            {modelConfigRef ? (
              <div>
                <dt className="sr-only">Model config</dt>
                <dd>
                  <span className="text-muted-foreground/80">Model config:</span>{" "}
                  <span className="font-mono text-foreground/90">{modelConfigRef}</span>
                </dd>
              </div>
            ) : null}
            {gatewaySandboxName ? (
              <div>
                <dt className="sr-only">Gateway sandbox</dt>
                <dd>
                  <span className="text-muted-foreground/80">Gateway:</span>{" "}
                  <span className="font-mono text-foreground/90">{gatewaySandboxName}</span>
                </dd>
              </div>
            ) : null}
          </dl>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {showConnectButton ? (
            <Button type="button" size="sm" variant="secondary" onClick={() => connectTerminal(gatewaySandboxName)}>
              Connect
            </Button>
          ) : null}
          {sessionActive || connecting ? (
            <Button type="button" size="sm" variant="outline" onClick={onDisconnect}>
              {connecting ? "Cancel" : "Disconnect"}
            </Button>
          ) : null}
        </div>
      </header>

      {!gatewaySandboxName ? (
        <p className="text-sm text-muted-foreground">
          Open an OpenShell sandbox from the <span className="text-foreground">Agents</span> list to start a terminal
          session.
        </p>
      ) : null}

      {termError ? <p className="text-sm text-destructive">{termError}</p> : null}

      <div
        ref={termHostRef}
        className="min-h-[min(520px,70vh)] w-full flex-1 rounded-md border border-border/80 bg-[#0c0c0c]"
        aria-label="Sandbox SSH terminal"
      />
    </div>
  );
}
