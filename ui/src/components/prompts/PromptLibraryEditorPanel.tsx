"use client";

import Link from "next/link";
import { Button } from "@/components/ui/button";
import { FormSection } from "@/components/agent-form/form-primitives";
import { FragmentEntriesEditor, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ArrowLeft, Loader2, Trash2 } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";

export type PromptLibraryEditorPanelProps = {
  namespace: string;
  name: string;
  rows: FragmentRow[];
  saving: boolean;
  confirmOpen: boolean;
  listHref: string;
  onRowsChange: (rows: FragmentRow[]) => void;
  onSave: () => void;
  onDeleteClick: () => void;
  onConfirmDelete: () => void;
  onConfirmOpenChange: (open: boolean) => void;
};

export function PromptLibraryEditorPanel({
  namespace,
  name,
  rows,
  saving,
  confirmOpen,
  listHref,
  onRowsChange,
  onSave,
  onDeleteClick,
  onConfirmDelete,
  onConfirmOpenChange,
}: PromptLibraryEditorPanelProps) {
  return (
    <AppPageFrame ariaLabelledBy="prompt-lib-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <Link
        href={listHref}
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
            onClick={onDeleteClick}
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
            void onSave();
          }}
        >
          <FragmentEntriesEditor rows={rows} onRowsChange={onRowsChange} disabled={saving} />
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
        onOpenChange={onConfirmOpenChange}
        title="Delete this prompt library?"
        description="Agents that reference it as a prompt source may fail until you update them."
        confirmLabel="Delete library"
        onConfirm={onConfirmDelete}
      />
    </AppPageFrame>
  );
}
