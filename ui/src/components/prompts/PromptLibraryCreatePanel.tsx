"use client";

import Link from "next/link";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { ArrowLeft, Loader2 } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { FormSection } from "@/components/agent-form/form-primitives";
import { FragmentEntriesEditor, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";

export type PromptLibraryCreatePanelProps = {
  namespace: string;
  name: string;
  rows: FragmentRow[];
  saving: boolean;
  backHref: string;
  cancelHref: string;
  onNamespaceChange: (value: string) => void;
  onNameChange: (value: string) => void;
  onRowsChange: (rows: FragmentRow[]) => void;
  onSubmit: (e: React.FormEvent) => void;
};

export function PromptLibraryCreatePanel({
  namespace,
  name,
  rows,
  saving,
  backHref,
  cancelHref,
  onNamespaceChange,
  onNameChange,
  onRowsChange,
  onSubmit,
}: PromptLibraryCreatePanelProps) {
  return (
    <AppPageFrame ariaLabelledBy="new-prompt-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <div>
        <Link
          href={backHref}
          className="mb-8 inline-flex items-center gap-2 rounded-sm text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to prompt libraries
        </Link>

        <PageHeader titleId="new-prompt-title" title="New Prompt Library" className="mb-8" />

        <form onSubmit={onSubmit} className="space-y-8" noValidate>
          <div className="grid gap-6 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="pl-ns">Namespace</Label>
              <NamespaceCombobox id="pl-ns" value={namespace} onValueChange={onNamespaceChange} placeholder="Namespace…" />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pl-name">Name</Label>
              <Input
                id="pl-name"
                name="configMapName"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder="e.g. team-prompts…"
                autoComplete="off"
                spellCheck={false}
                translate="no"
                aria-describedby="pl-name-hint"
              />
            </div>
          </div>
          <p id="pl-name-hint" className="sr-only">
            Kubernetes resource name: lowercase letters, numbers, hyphens, periods.
          </p>

          <FormSection
            title="Fragment keys"
            description="Each key becomes a fragment you can include with an include tag or the mention picker in agent instructions."
          >
            <p className="text-xs text-muted-foreground">
              Reference in agents as{" "}
              <code className="font-mono text-[11px]" translate="no">{`{{include "name/key"}}`}</code>.
            </p>
            <FragmentEntriesEditor rows={rows} onRowsChange={onRowsChange} disabled={saving} />
          </FormSection>

          <div className="flex flex-col gap-3 border-t border-border/50 pt-6 sm:flex-row sm:items-center sm:justify-between">
            <Button type="button" variant="outline" asChild className="w-full sm:w-auto">
              <Link href={cancelHref}>Cancel</Link>
            </Button>
            <Button type="submit" size="lg" className="min-w-[10rem] w-full sm:w-auto" disabled={saving} aria-busy={saving}>
              {saving ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 shrink-0 animate-spin" aria-hidden />
                  Creating…
                </>
              ) : (
                "Create Library"
              )}
            </Button>
          </div>
        </form>
      </div>
    </AppPageFrame>
  );
}
