"use client";

import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { getPromptTemplate, updatePromptTemplate, deletePromptTemplate } from "@/app/actions/promptTemplates";
import { LoadingState } from "@/components/LoadingState";
import { rowsFromData, dataFromRows, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { toast } from "sonner";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PromptLibraryEditorPanel } from "@/components/prompts/PromptLibraryEditorPanel";

export default function PromptDetailPage({
  params,
}: {
  params: Promise<{ namespace: string; name: string }>;
}) {
  const { namespace, name } = use(params);
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [rows, setRows] = useState<FragmentRow[]>(() => rowsFromData({}));
  const [saving, setSaving] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      const res = await getPromptTemplate(namespace, name);
      if (cancelled) {
        return;
      }
      if (res.error || !res.data) {
        toast.error(res.error || "Could not load prompt library");
        setLoading(false);
        return;
      }
      setRows(rowsFromData(res.data.data));
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [namespace, name]);

  const handleSave = async () => {
    const data = dataFromRows(rows);
    if (Object.keys(data).length === 0) {
      toast.error("At least one key is required");
      return;
    }
    const keys = Object.keys(data);
    const dup = keys.find((k, i) => keys.indexOf(k) !== i);
    if (dup) {
      toast.error(`Duplicate key: ${dup}`);
      return;
    }
    setSaving(true);
    const res = await updatePromptTemplate(namespace, name, data);
    setSaving(false);
    if (res.error) {
      toast.error(res.error);
      return;
    }
    toast.success("Saved");
  };

  const handleDelete = async () => {
    setSaving(true);
    const res = await deletePromptTemplate(namespace, name);
    setSaving(false);
    if (res.error) {
      toast.error(res.error);
      return;
    }
    toast.success("Prompt library deleted");
    router.push(`/prompts?namespace=${encodeURIComponent(namespace)}`);
  };

  if (loading) {
    return (
      <AppPageFrame mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
        <div className="relative" role="status" aria-live="polite" aria-busy="true">
          <span className="sr-only">Loading prompt library…</span>
          <LoadingState />
        </div>
      </AppPageFrame>
    );
  }

  return (
    <PromptLibraryEditorPanel
      namespace={namespace}
      name={name}
      rows={rows}
      saving={saving}
      confirmOpen={confirmOpen}
      listHref={`/prompts?namespace=${encodeURIComponent(namespace)}`}
      onRowsChange={setRows}
      onSave={handleSave}
      onDeleteClick={() => setConfirmOpen(true)}
      onConfirmDelete={handleDelete}
      onConfirmOpenChange={setConfirmOpen}
    />
  );
}
