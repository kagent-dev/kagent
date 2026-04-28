"use client";

import { Suspense, startTransition, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState } from "@/components/LoadingState";
import { createPromptTemplate } from "@/app/actions/promptTemplates";
import { FragmentEntriesEditor, rowsFromData, dataFromRows, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { isResourceNameValid } from "@/lib/utils";
import { toast } from "sonner";
import { ArrowLeft } from "lucide-react";

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
    <div className="min-h-screen p-8">
      <div className="max-w-3xl mx-auto">
        <Link
          href={namespace ? `/prompts?namespace=${encodeURIComponent(namespace)}` : "/prompts"}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-8 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to prompt libraries
        </Link>

        <h1 className="text-2xl font-bold mb-8">New prompt library</h1>

        <form onSubmit={handleSubmit} className="space-y-6">
          <div className="grid gap-6 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="pl-ns">Namespace</Label>
              <NamespaceCombobox value={namespace} onValueChange={setNamespace} placeholder="Namespace…" />
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
              />
            </div>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-xl font-bold">Fragment keys</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <p className="text-xs text-muted-foreground">
                Each key becomes a fragment you can include with{" "}
                <code className="font-mono text-[11px]" translate="no">{`{{include "name/key"}}`}</code>.
              </p>
              <FragmentEntriesEditor rows={rows} onRowsChange={setRows} disabled={saving} />
            </CardContent>
          </Card>

          <div className="flex gap-3">
            <Button type="submit" disabled={saving}>
              {saving ? "Saving…" : "Create library"}
            </Button>
            <Button type="button" variant="outline" asChild>
              <Link href={namespace ? `/prompts?namespace=${encodeURIComponent(namespace)}` : "/prompts"}>Cancel</Link>
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default function NewPromptPage() {
  return (
    <Suspense fallback={<LoadingState />}>
      <NewPromptContent />
    </Suspense>
  );
}
