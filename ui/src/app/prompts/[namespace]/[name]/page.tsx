"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { getPromptTemplate, updatePromptTemplate, deletePromptTemplate } from "@/app/actions/promptTemplates";
import { Button } from "@/components/ui/button";
import { FormSection } from "@/components/agent-form/form-primitives";
import { LoadingState } from "@/components/LoadingState";
import { FragmentEntriesEditor, rowsFromData, dataFromRows, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { toast } from "sonner";
import { ArrowLeft, Loader2, Trash2 } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";

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
    <AppPageFrame ariaLabelledBy="prompt-lib-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
        <Link
          href={`/prompts?namespace=${encodeURIComponent(namespace)}`}
          className="mb-8 inline-flex items-center gap-2 rounded-sm text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to prompt libraries
        </Link>

        <PageHeader
          titleId="prompt-lib-title"
          title={name}
          isMonospaceTitle
          description={
            <>
              Namespace <span className="font-mono text-foreground" translate="no">{namespace}</span>
            </>
          }
          className="mb-8"
          end={
            <Button
              type="button"
              variant="outline"
              className="w-full gap-2 border-destructive/40 text-destructive hover:bg-destructive/10 sm:w-auto"
              onClick={() => setConfirmOpen(true)}
              disabled={saving}
            >
              <Trash2 className="h-4 w-4" aria-hidden />
              Delete
            </Button>
          }
        />

        <FormSection
          title="Data"
          description="Named keys become include targets for agents. Save to update the config map in the cluster."
        >
            <form
              className="space-y-6"
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                void handleSave();
              }}
            >
            <FragmentEntriesEditor rows={rows} onRowsChange={setRows} disabled={saving} />
            <div className="flex justify-end border-t border-border/50 pt-6">
              <Button type="submit" size="lg" className="min-w-[10rem]" disabled={saving} aria-busy={saving}>
                {saving ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 shrink-0 animate-spin" aria-hidden />
                    Saving…
                  </>
                ) : (
                  "Save changes"
                )}
              </Button>
            </div>
            </form>
        </FormSection>

        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title="Delete this prompt library?"
          description="Agents that reference it as a prompt source may fail until you update them."
          confirmLabel="Delete library"
          onConfirm={handleDelete}
        />
    </AppPageFrame>
  );
}
