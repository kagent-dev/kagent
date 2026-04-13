"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { getPromptTemplate, updatePromptTemplate, deletePromptTemplate } from "@/app/actions/promptTemplates";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState } from "@/components/LoadingState";
import { FragmentEntriesEditor, rowsFromData, dataFromRows, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { toast } from "sonner";
import { ArrowLeft, Trash2, Boxes } from "lucide-react";

export default function PromptDetailPage({
  params,
}: {
  params: Promise<{ namespace: string; name: string }>;
}) {
  const { namespace, name } = use(params);
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [readOnly, setReadOnly] = useState(false);
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
      setReadOnly(res.data.readOnly);
      setRows(rowsFromData(res.data.data));
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [namespace, name]);

  const handleSave = async () => {
    if (readOnly) {
      return;
    }
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
    return <LoadingState />;
  }

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-3xl mx-auto">
        <Link
          href={`/prompts?namespace=${encodeURIComponent(namespace)}`}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-8 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to prompt libraries
        </Link>

        <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-2 min-w-0">
            <h1 className="text-2xl font-bold font-mono break-all" translate="no">
              {name}
            </h1>
            <p className="text-sm text-muted-foreground">
              Namespace <span className="font-mono text-foreground">{namespace}</span>
            </p>
            {readOnly && (
              <p className="text-sm text-amber-700 dark:text-amber-400" role="status">
                This prompt library is read-only in the UI (for example the cluster built-in bundle). Copy content into
                your own library to edit.
              </p>
            )}
          </div>
          {!readOnly && (
            <Button
              type="button"
              variant="outline"
              className="gap-2 border-destructive/40 text-destructive hover:bg-destructive/10 self-start"
              onClick={() => setConfirmOpen(true)}
              disabled={saving}
            >
              <Trash2 className="h-4 w-4" aria-hidden />
              Delete
            </Button>
          )}
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-xl font-bold">
              <Boxes className="h-5 w-5" aria-hidden />
              Data
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-6">
            <FragmentEntriesEditor rows={rows} onRowsChange={setRows} readOnly={readOnly} disabled={saving} />
            {!readOnly && (
              <div className="flex gap-3">
                <Button type="button" onClick={handleSave} disabled={saving}>
                  {saving ? "Saving…" : "Save changes"}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title="Delete this prompt library?"
          description="Agents that reference it as a prompt source may fail until you update them."
          confirmLabel="Delete library"
          onConfirm={handleDelete}
        />
      </div>
    </div>
  );
}
