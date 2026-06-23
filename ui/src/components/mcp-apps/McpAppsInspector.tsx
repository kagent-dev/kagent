"use client";

import { useEffect, useMemo, useState } from "react";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { AlertCircle, Loader2, Play } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import {
  callMcpAppTool,
  listMcpAppTools,
  type McpAppTool,
} from "@/app/actions/mcp-apps";
import { McpAppRenderer } from "./McpAppRenderer";

interface McpAppsInspectorProps {
  namespace: string;
  name: string;
}

type UiMcpAppTool = McpAppTool & { uiResourceUri: string };

export function McpAppsInspector({ namespace, name }: McpAppsInspectorProps) {
  const [tools, setTools] = useState<UiMcpAppTool[]>([]);
  const [selectedToolName, setSelectedToolName] = useState<string>("");
  const [argsText, setArgsText] = useState("{}");
  const [toolResult, setToolResult] = useState<CallToolResult | undefined>();
  const [toolInput, setToolInput] = useState<Record<string, unknown> | undefined>();
  const [loading, setLoading] = useState(true);
  const [invoking, setInvoking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function loadTools() {
      setLoading(true);
      setError(null);
      const response = await listMcpAppTools(namespace, name);
      if (cancelled) {
        return;
      }
      if (response.error || !response.data) {
        setError(response.error || response.message);
        setTools([]);
      } else {
        const uiTools = response.data.filter((tool): tool is UiMcpAppTool => !!tool.uiResourceUri);
        setTools(uiTools);
        setSelectedToolName((current) => current || uiTools[0]?.name || "");
      }
      setLoading(false);
    }
    void loadTools();
    return () => {
      cancelled = true;
    };
  }, [namespace, name]);

  const selectedTool = useMemo(
    () => tools.find((tool) => tool.name === selectedToolName),
    [selectedToolName, tools],
  );

  const invokeSelectedTool = async () => {
    if (!selectedTool) {
      return;
    }
    let args: Record<string, unknown>;
    try {
      const parsed = JSON.parse(argsText || "{}") as unknown;
      if (parsed === null || Array.isArray(parsed) || typeof parsed !== "object") {
        throw new Error("Tool arguments must be a JSON object");
      }
      args = parsed as Record<string, unknown>;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid JSON arguments");
      return;
    }

    setInvoking(true);
    setError(null);
    setToolResult(undefined);
    setToolInput(args);
    const response = await callMcpAppTool(namespace, name, selectedTool.name, args);
    if (response.error || !response.data) {
      setError(response.error || response.message);
    } else {
      setToolResult(response.data);
    }
    setInvoking(false);
  };

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-8 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">MCP Apps</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Inspect UI-capable tools exposed by <span className="font-mono">{namespace}/{name}</span>.
        </p>
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>UI-capable tools</CardTitle>
          <CardDescription>
            Tools are listed only when they declare <code>_meta.ui.resourceUri</code> or the legacy alias.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading MCP Apps...
            </div>
          ) : tools.length === 0 ? (
            <div className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
              No MCP Apps found for this server.
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              {tools.map((tool) => (
                <button
                  key={tool.name}
                  type="button"
                  className={`rounded-lg border p-4 text-left transition-colors ${
                    selectedToolName === tool.name ? "border-primary bg-primary/5" : "hover:bg-muted/40"
                  }`}
                  onClick={() => {
                    setSelectedToolName(tool.name);
                    setToolResult(undefined);
                  }}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="font-medium" translate="no">{tool.name}</div>
                      {tool.description ? (
                        <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{tool.description}</p>
                      ) : null}
                    </div>
                    <Badge variant="secondary">MCP App</Badge>
                  </div>
                  <div className="mt-3 truncate text-xs text-muted-foreground" title={tool.uiResourceUri}>
                    {tool.uiResourceUri}
                  </div>
                </button>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {selectedTool ? (
        <Card>
          <CardHeader>
            <CardTitle>Invoke {selectedTool.name}</CardTitle>
            <CardDescription>Enter JSON arguments, invoke the tool, then render its MCP App resource.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Textarea
              value={argsText}
              onChange={(event) => setArgsText(event.target.value)}
              className="min-h-32 font-mono text-sm"
              spellCheck={false}
              aria-label="Tool arguments JSON"
            />
            <Button type="button" onClick={invokeSelectedTool} disabled={invoking}>
              {invoking ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Play className="mr-2 h-4 w-4" />}
              Invoke tool
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {selectedTool && toolResult ? (
        <McpAppRenderer
          namespace={namespace}
          serverName={name}
          toolName={selectedTool.name}
          toolResourceUri={selectedTool.uiResourceUri}
          toolInput={toolInput}
          toolResult={toolResult}
        />
      ) : null}
    </div>
  );
}
