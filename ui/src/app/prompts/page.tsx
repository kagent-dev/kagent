"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { listPromptTemplates } from "@/app/actions/promptTemplates";
import type { PromptTemplateSummary } from "@/types";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/LoadingState";
import { ScrollText, Plus, Lock, ChevronRight } from "lucide-react";
import { toast } from "sonner";

export default function PromptsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";
  const [items, setItems] = useState<PromptTemplateSummary[] | null>(null);
  const [loading, setLoading] = useState(true);

  const syncNsToUrl = useCallback(
    (ns: string) => {
      const q = new URLSearchParams(searchParams.toString());
      if (ns) {
        q.set("namespace", ns);
      } else {
        q.delete("namespace");
      }
      router.replace(`/prompts?${q.toString()}`, { scroll: false });
    },
    [router, searchParams],
  );

  const handleNamespaceChange = (ns: string) => {
    syncNsToUrl(ns);
  };

  useEffect(() => {
    if (!namespace) {
      return;
    }
    let cancelled = false;
    (async () => {
      setLoading(true);
      const res = await listPromptTemplates(namespace);
      if (cancelled) {
        return;
      }
      if (res.error || !res.data) {
        toast.error(res.error || "Could not load prompt libraries");
        setItems([]);
      } else {
        setItems(res.data);
      }
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [namespace]);

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-6xl mx-auto">
        <header className="mb-8 flex flex-col gap-6 md:flex-row md:items-end md:justify-between">
          <div className="space-y-2">
            <h1 className="text-2xl font-bold">Prompt libraries</h1>
            <p className="text-sm text-muted-foreground max-w-2xl">
              Each library holds multiple named prompt fragments (keys). Reference them from agents with{" "}
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs" translate="no">
                {`{{include "name/key"}}`}
              </code>{" "}
              or type <kbd className="font-mono text-xs">@</kbd> in agent instructions to pick a key. Built-in and
              UI-created libraries are marked when applicable.
            </p>
          </div>
          <div className="flex flex-col sm:flex-row gap-3 sm:items-center">
            <div className="w-full sm:w-64">
              <label className="sr-only" htmlFor="prompts-namespace">
                Namespace
              </label>
              <NamespaceCombobox value={namespace} onValueChange={handleNamespaceChange} placeholder="Namespace…" />
            </div>
            <Button asChild className="gap-2">
              <Link href={namespace ? `/prompts/new?ns=${encodeURIComponent(namespace)}` : "/prompts/new"}>
                <Plus className="h-4 w-4" aria-hidden />
                New prompt library
              </Link>
            </Button>
          </div>
        </header>

        {!namespace && (
          <p className="text-sm text-muted-foreground" role="status">
            Choose a namespace to list prompt libraries…
          </p>
        )}

        {namespace && loading && <LoadingState />}

        {namespace && !loading && items && items.length === 0 && (
          <div
            className="rounded-lg border border-dashed border-border px-6 py-12 text-center"
            role="status"
          >
            <ScrollText className="mx-auto h-10 w-10 text-muted-foreground mb-4" aria-hidden />
            <p className="text-base font-medium mb-1">No prompt libraries in this namespace</p>
            <p className="text-sm text-muted-foreground mb-6 max-w-md mx-auto">
              Create one here, or add libraries with <code className="font-mono text-xs">kubectl</code>. Install kagent in
              this namespace to see built-in libraries when present.
            </p>
            <Button asChild variant="secondary">
              <Link href={`/prompts/new?ns=${encodeURIComponent(namespace)}`}>Create library</Link>
            </Button>
          </div>
        )}

        {namespace && !loading && items && items.length > 0 && (
          <ul className="grid gap-4 sm:grid-cols-2">
            {items.map((cm) => (
              <li key={`${cm.namespace}/${cm.name}`}>
                <Link
                  href={`/prompts/${encodeURIComponent(cm.namespace)}/${encodeURIComponent(cm.name)}`}
                  className="group block h-full rounded-lg border bg-card p-5 shadow-sm transition-shadow hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="font-mono text-sm font-medium truncate" translate="no">
                        {cm.name}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        <span className="tabular-nums">{cm.keyCount}</span> keys
                      </p>
                    </div>
                    {cm.readOnly ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground shrink-0">
                        <Lock className="h-3 w-3" aria-hidden />
                        Read-only
                      </span>
                    ) : (
                      <ChevronRight
                        className="h-5 w-5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 motion-reduce:opacity-100"
                        aria-hidden
                      />
                    )}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
