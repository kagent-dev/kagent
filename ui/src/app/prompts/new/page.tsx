"use client";

import { Suspense, startTransition, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/LoadingState";
import { createPromptTemplate } from "@/app/actions/promptTemplates";
import { FragmentEntriesEditor, rowsFromData, dataFromRows, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { isResourceNameValid } from "@/lib/utils";
import { toast } from "sonner";
import { ArrowLeft, Loader2 } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { FormSection } from "@/components/agent-form/form-primitives";

function NewPromptContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [namespace, setNamespace] = useState(searchParams.get("ns") || "");
  const [name, setName] = useState("");
  const [rows, setRows] = useState<FragmentRow[]>(() => rowsFromData({}));
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const n = searchParams.get("ns");
    if (n) {
      startTransition(() => {
        setNamespace(n);
      });
    }
  }, [searchParams]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmedName = name.trim();
    if (!namespace.trim()) {
      toast.error("Select a namespace");
      return;
    }
    if (!trimmedName) {
      toast.error("Library name is required");
      return;
    }
    if (!isResourceNameValid(trimmedName)) {
      toast.error("Name must be a valid Kubernetes resource name");
      return;
    }
    const data = dataFromRows(rows);
    const keys = Object.keys(data);
    if (keys.length === 0) {
      toast.error("Add at least one key");
      return;
    }
    const dup = keys.find((k, i) => keys.indexOf(k) !== i);
    if (dup) {
      toast.error(`Duplicate key: ${dup}`);
      return;
    }

    setSaving(true);
    const res = await createPromptTemplate({ namespace: namespace.trim(), name: trimmedName, data });
    setSaving(false);
    if (res.error || !res.data) {
      toast.error(res.error || "Could not create prompt library");
      return;
    }
    toast.success("Prompt library created");
    router.push(`/prompts/${encodeURIComponent(res.data.namespace)}/${encodeURIComponent(res.data.name)}`);
  };

  return (
    <AppPageFrame ariaLabelledBy="new-prompt-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <div>
        <Link
          href={namespace ? `/prompts?namespace=${encodeURIComponent(namespace)}` : "/prompts"}
          className="mb-8 inline-flex items-center gap-2 rounded-sm text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to prompt libraries
        </Link>

        <PageHeader titleId="new-prompt-title" title="New Prompt Library" className="mb-8" />

        <form onSubmit={handleSubmit} className="space-y-8" noValidate>
          <div className="grid gap-6 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="pl-ns">Namespace</Label>
              <NamespaceCombobox
                id="pl-ns"
                value={namespace}
                onValueChange={setNamespace}
                placeholder="Namespace…"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pl-name">Name</Label>
              <Input
                id="pl-name"
                name="configMapName"
                value={name}
                onChange={(e) => setName(e.target.value)}
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
            <FragmentEntriesEditor rows={rows} onRowsChange={setRows} disabled={saving} />
          </FormSection>

          <div className="flex flex-col gap-3 border-t border-border/50 pt-6 sm:flex-row sm:items-center sm:justify-between">
            <Button type="button" variant="outline" asChild className="w-full sm:w-auto">
              <Link href={namespace ? `/prompts?namespace=${encodeURIComponent(namespace)}` : "/prompts"}>Cancel</Link>
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

export default function NewPromptPage() {
  return (
    <Suspense fallback={<LoadingState />}>
      <NewPromptContent />
    </Suspense>
  );
}
