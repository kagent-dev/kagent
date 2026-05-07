"use client";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
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
  const clawHarnessSession = searchParams.get("clawHarness") === "1";
  const autoConnect = Boolean(gatewaySandboxName);
  const namespace = searchParams.get("ns")?.trim() ?? "";
  const crName = searchParams.get("name")?.trim() ?? "";
  const modelConfigRef = searchParams.get("modelConfigRef")?.trim() ?? "";
  const [plainShellOnly, setPlainShellOnly] = useState(() => searchParams.get("plainShell") === "1");
  /** Plain-shell mode the active SSH session was opened with (null when disconnected). */
  const [appliedPlainShell, setAppliedPlainShell] = useState<boolean | null>(null);

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
        setAppliedPlainShell(plainShellOnly);
        term.reset();
        ws.send(
          JSON.stringify({
            sandbox_name: name,
            plain_shell: plainShellOnly,
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
        setAppliedPlainShell(null);
        term.writeln(`\r\n\x1b[90m(disconnected)\x1b[0m`);
        if (!ev.wasClean && ev.code === 1006) {
          setTermError("Connection closed abnormally (1006).");
        }
      };
    },
    [plainShellOnly],
  );

  const restartSession = useCallback(() => {
    const name = gatewaySandboxName.trim();
    if (!name) return;
    wsRef.current?.close();
    window.setTimeout(() => connectTerminal(name), 120);
  }, [gatewaySandboxName, connectTerminal]);

  useEffect(() => {
    if (!autoConnect || !gatewaySandboxName) return;
    const t = window.setTimeout(() => {
      if (!termRef.current) return;
      connectTerminal(gatewaySandboxName);
    }, 400);
    return () => window.clearTimeout(t);
  }, [autoConnect, gatewaySandboxName, connectTerminal]);

  const showReconnect = Boolean(gatewaySandboxName) && !sessionActive && !connecting;
  const plainShellPendingRestart =
    clawHarnessSession &&
    sessionActive &&
    appliedPlainShell !== null &&
    plainShellOnly !== appliedPlainShell;

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
        <div className="flex shrink-0 flex-col items-end gap-2 sm:flex-row sm:items-start">
          {clawHarnessSession && gatewaySandboxName ? (
            <div className="flex max-w-[min(100%,260px)] flex-col gap-1 rounded-md border border-border/60 bg-muted/20 px-3 py-2">
              <div className="flex items-start gap-2">
                <Checkbox
                  id="openshell-plain-shell"
                  checked={plainShellOnly}
                  onCheckedChange={(v) => setPlainShellOnly(v === true)}
                  disabled={connecting}
                />
                <Label htmlFor="openshell-plain-shell" className="cursor-pointer text-sm font-normal leading-snug text-foreground">
                  Launch plain shell
                </Label>
              </div>
              {plainShellPendingRestart ? (
                <p className="pl-7 text-xs text-muted-foreground">Restart session to apply.</p>
              ) : null}
            </div>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            {showReconnect ? (
              <Button type="button" size="sm" variant="secondary" onClick={() => connectTerminal(gatewaySandboxName)}>
                Reconnect
              </Button>
            ) : null}
            {sessionActive ? (
              <Button type="button" size="sm" variant="secondary" onClick={restartSession}>
                Restart
              </Button>
            ) : null}
            {connecting ? (
              <Button type="button" size="sm" variant="outline" onClick={onDisconnect}>
                Cancel
              </Button>
            ) : null}
          </div>
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
        className="w-full flex-1 rounded-md border border-border/80 bg-[#0c0c0c] p-3 md:p-4"
        aria-label="Sandbox SSH terminal container"
      >
        <div
          ref={termHostRef}
          className="min-h-[min(620px,78vh)] w-full"
          aria-label="Sandbox SSH terminal"
        />
      </div>
    </div>
  );
}
