"use client";

import Link from "next/link";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import type { PromptTemplateSummary } from "@/types";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/LoadingState";
import { ScrollText, Plus, ChevronRight } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";

export type PromptLibrariesPanelProps = {
  namespace: string;
  loading: boolean;
  items: PromptTemplateSummary[] | null;
  onNamespaceChange: (ns: string) => void;
};

export function PromptLibrariesPanel({ namespace, loading, items, onNamespaceChange }: PromptLibrariesPanelProps) {
  return (
    <div>
      <PageHeader
        titleId="prompts-page-title"
        title="Prompt Libraries"
        description={
          <>
            Each library holds multiple named prompt fragments (keys). Reference them from agents with{" "}
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs" translate="no">
              {`{{include "name/key"}}`}
            </code>{" "}
            or type <kbd>@</kbd> in agent instructions to pick a key.
          </>
        }
        className="mb-8"
        end={
          <div className="flex w-full flex-col gap-3 sm:w-auto sm:min-w-[20rem] sm:flex-row sm:items-center">
            <div className="w-full sm:w-64">
              <label className="sr-only" htmlFor="prompts-namespace">
                Namespace
              </label>
              <NamespaceCombobox
                id="prompts-namespace"
                value={namespace}
                onValueChange={onNamespaceChange}
                placeholder="Namespace…"
              />
            </div>
            <Button asChild className="gap-2" size="lg">
              <Link href={namespace ? `/prompts/new?ns=${encodeURIComponent(namespace)}` : "/prompts/new"}>
                <Plus className="h-4 w-4" aria-hidden />
                New Library
              </Link>
            </Button>
          </div>
        }
      />

      {!namespace && (
        <p className="text-sm text-muted-foreground" role="status">
          Choose a namespace to list prompt libraries…
        </p>
      )}

      {namespace && loading && <LoadingState />}

      {namespace && !loading && items && items.length === 0 && (
        <div
          className="rounded-xl border border-dashed border-border/60 bg-card/20 px-6 py-12 text-center"
          role="status"
        >
          <ScrollText className="mx-auto mb-4 h-10 w-10 text-muted-foreground" aria-hidden />
          <p className="mb-1 text-base font-medium">No prompt libraries in this namespace</p>
          <p className="mx-auto mb-6 max-w-md text-sm text-muted-foreground">
            Create one here, or add libraries with <code className="font-mono text-xs">kubectl</code>. Install kagent in this
            namespace to see built-in libraries when present.
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
                className="group block h-full rounded-xl border border-border/60 bg-card/80 p-5 shadow-sm transition-shadow hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate font-mono text-sm font-medium" translate="no">
                      {cm.name}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      <span className="tabular-nums">{cm.keyCount}</span> keys
                    </p>
                  </div>
                  <ChevronRight
                    className="h-5 w-5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 motion-reduce:opacity-100"
                    aria-hidden
                  />
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
