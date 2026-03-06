"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Activity,
  CheckCircle2,
  XCircle,
  AlertCircle,
  RefreshCw,
  ExternalLink,
  Puzzle,
  Server,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  getPlugins,
  checkPluginBackend,
  type PluginItem,
  type PluginBackendStatus,
} from "../actions/plugins";
import Link from "next/link";

interface PluginWithStatus extends PluginItem {
  backendStatus: PluginBackendStatus;
  statusCode?: number;
}

export default function PluginsStatusPage() {
  const [plugins, setPlugins] = useState<PluginWithStatus[]>([]);
  const [apiError, setApiError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);

  const loadPlugins = useCallback(async () => {
    setLoading(true);
    setApiError(null);
    const result = await getPlugins();
    if (result.error || !result.data) {
      setApiError(result.error ?? result.message ?? "Failed to load plugins");
      setPlugins([]);
      setLoading(false);
      return;
    }
    setPlugins(
      result.data.map((p) => ({ ...p, backendStatus: "checking" as PluginBackendStatus }))
    );
    setLoading(false);
  }, []);

  useEffect(() => {
    loadPlugins();
  }, [loadPlugins]);

  const runHealthChecks = useCallback(async () => {
    setChecking(true);
    setPlugins((prev) =>
      prev.map((p) => ({ ...p, backendStatus: "checking" as PluginBackendStatus }))
    );
    const results = await Promise.all(
      plugins.map(async (p) => {
        const { status, statusCode } = await checkPluginBackend(p.pathPrefix);
        return { ...p, backendStatus: status, statusCode };
      })
    );
    setPlugins(results);
    setChecking(false);
  }, [plugins]);

  useEffect(() => {
    if (plugins.length === 0 || plugins.every((p) => p.backendStatus !== "checking")) return;
    let cancelled = false;
    const list = plugins;
    Promise.all(
      list.map(async (p) => {
        if (cancelled) return p;
        const { status, statusCode } = await checkPluginBackend(p.pathPrefix);
        return { ...p, backendStatus: status, statusCode };
      })
    ).then((results) => {
      if (!cancelled) setPlugins(results);
    });
    return () => {
      cancelled = true;
    };
  }, [plugins]);

  if (loading) {
    return (
      <div className="mt-12 mx-auto max-w-4xl px-6">
        <div className="flex flex-col items-center justify-center h-[200px] border rounded-lg bg-secondary/5">
          <div className="animate-pulse h-8 w-8 rounded-full bg-primary/20 mb-4" />
          <p className="text-muted-foreground">Loading plugin registry…</p>
        </div>
      </div>
    );
  }

  return (
    <div className="mt-12 mx-auto max-w-4xl px-6">
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold flex items-center gap-2">
              <Puzzle className="h-7 w-7" />
              Plugins status
            </h1>
            <p className="text-muted-foreground mt-1">
              Internal view of <code className="text-xs bg-muted px-1 rounded">/api/plugins</code> and
              backend proxy health.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => loadPlugins()}
              disabled={loading}
            >
              <RefreshCw className="h-4 w-4 mr-2" />
              Refresh list
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={runHealthChecks}
              disabled={checking || plugins.length === 0}
            >
              <Activity className="h-4 w-4 mr-2" />
              {checking ? "Checking…" : "Re-check backends"}
            </Button>
          </div>
        </div>

        {apiError && (
          <Card className="border-destructive/50 bg-destructive/5">
            <CardHeader>
              <CardTitle className="text-destructive flex items-center gap-2">
                <AlertCircle className="h-5 w-5" />
                API error
              </CardTitle>
              <CardDescription>{apiError}</CardDescription>
            </CardHeader>
          </Card>
        )}

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Server className="h-5 w-5" />
              Plugin registry
            </CardTitle>
            <CardDescription>
              Plugins with UI enabled are listed by the backend from <code className="text-xs bg-muted px-1 rounded">GET /api/plugins</code>.
              Backend status is checked by requesting <code className="text-xs bg-muted px-1 rounded">/_p/&#123;pathPrefix&#125;/</code>.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {plugins.length === 0 && !apiError ? (
              <p className="text-muted-foreground py-6 text-center">
                No plugins with UI registered. Deploy a RemoteMCPServer with{" "}
                <code className="text-xs bg-muted px-1 rounded">spec.ui.enabled: true</code>.
              </p>
            ) : (
              <div className="space-y-3">
                {plugins.map((p) => (
                  <div
                    key={p.pathPrefix}
                    className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="flex flex-col gap-1">
                        <div className="font-medium">{p.displayName}</div>
                        <div className="text-xs text-muted-foreground font-mono">
                          {p.name} · pathPrefix: {p.pathPrefix} · section: {p.section}
                        </div>
                      </div>
                      <StatusBadge status={p.backendStatus} statusCode={p.statusCode} />
                    </div>
                    <div className="flex items-center gap-2">
                      <Link href={`/plugins/${p.pathPrefix}`}>
                        <Button variant="outline" size="sm">
                          <ExternalLink className="h-4 w-4 mr-2" />
                          Open plugin
                        </Button>
                      </Link>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function StatusBadge({
  status,
  statusCode,
}: {
  status: PluginBackendStatus;
  statusCode?: number;
}) {
  if (status === "ok") {
    return (
      <Badge variant="default" className="bg-green-600 hover:bg-green-700 gap-1">
        <CheckCircle2 className="h-3 w-3" />
        Up
      </Badge>
    );
  }
  if (status === "not_found") {
    return (
      <Badge variant="secondary" className="gap-1">
        <XCircle className="h-3 w-3" />
        404
      </Badge>
    );
  }
  if (status === "unreachable") {
    return (
      <Badge variant="destructive" className="gap-1">
        <AlertCircle className="h-3 w-3" />
        {statusCode ? `${statusCode}` : "Unreachable"}
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="gap-1 animate-pulse">
      <Activity className="h-3 w-3" />
      Checking…
    </Badge>
  );
}
