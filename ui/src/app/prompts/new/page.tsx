"use client";

import { Suspense, startTransition, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/LoadingState";
import { createPromptTemplate } from "@/app/actions/promptTemplates";
import { rowsFromData, dataFromRows, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { isResourceNameValid } from "@/lib/utils";
import { toast } from "sonner";
import { PromptLibraryCreatePanel } from "@/components/prompts/PromptLibraryCreatePanel";

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

  const backHref = namespace ? `/prompts?namespace=${encodeURIComponent(namespace)}` : "/prompts";

  return (
    <PromptLibraryCreatePanel
      namespace={namespace}
      name={name}
      rows={rows}
      saving={saving}
      backHref={backHref}
      cancelHref={backHref}
      onNamespaceChange={setNamespace}
      onNameChange={setName}
      onRowsChange={setRows}
      onSubmit={handleSubmit}
    />
  );
}

export default function NewPromptPage() {
  return (
    <Suspense fallback={<LoadingState />}>
      <NewPromptContent />
    </Suspense>
  );
}
