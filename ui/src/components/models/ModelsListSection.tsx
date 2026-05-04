"use client";

import React from "react";
import { Button } from "@/components/ui/button";
import { ChevronDown, ChevronRight, Pencil, Plus, Trash2, Cpu } from "lucide-react";
import { ModelConfig, ModelConfigSpec } from "@/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { k8sRefUtils } from "@/lib/k8sUtils";

function getProviderParams(spec: ModelConfigSpec) {
  return (
    spec.openAI ??
    spec.anthropic ??
    spec.azureOpenAI ??
    spec.ollama ??
    spec.gemini ??
    spec.geminiVertexAI ??
    spec.anthropicVertexAI ??
    spec.bedrock ??
    spec.sapAICore ??
    undefined
  );
}

function safeRefParts(ref: string): { namespace: string; name: string } {
  try {
    return k8sRefUtils.fromRef(ref);
  } catch {
    return { namespace: "", name: ref };
  }
}

export type ModelsListSectionProps = {
  models: ModelConfig[];
  expandedRows: Set<string>;
  onToggleRow: (modelRef: string) => void;
  onEdit: (model: ModelConfig) => void;
  onRequestDelete: (model: ModelConfig) => void;
  modelToDelete: ModelConfig | null;
  onDismissDeleteDialog: () => void;
  onConfirmDelete: () => void | Promise<void>;
  onNewModel: () => void;
};

export function ModelsListSection({
  models,
  expandedRows,
  onToggleRow,
  onEdit,
  onRequestDelete,
  modelToDelete,
  onDismissDeleteDialog,
  onConfirmDelete,
  onNewModel,
}: ModelsListSectionProps) {
  return (
    <>
      {models.length === 0 ? (
        <div className="rounded-xl border border-border/60 bg-card/20 px-6 py-14 text-center shadow-sm">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted/60">
            <Cpu className="h-6 w-6 text-muted-foreground" aria-hidden />
          </div>
          <h2 className="mb-2 text-lg font-semibold tracking-tight text-foreground">No model configs</h2>
          <p className="mx-auto mb-6 max-w-md text-pretty text-sm text-muted-foreground">
            Create a namespaced model config so agents can resolve provider credentials and model IDs at runtime.
          </p>
          <Button size="lg" onClick={onNewModel}>
            <Plus className="mr-2 h-4 w-4" aria-hidden />
            New Model
          </Button>
        </div>
      ) : (
      <div className="overflow-hidden rounded-xl border border-border/60 bg-card/15 shadow-sm">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] border-separate border-spacing-0 text-sm">
            <caption className="sr-only">Model configurations</caption>
            <thead>
              <tr className="border-b border-border/50 bg-muted/25 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                <th scope="col" className="w-11 px-2 py-2.5 text-left font-medium">
                  <span className="sr-only">Expand</span>
                </th>
                <th scope="col" className="px-3 py-2.5 text-left font-medium">
                  Model
                </th>
                <th scope="col" className="min-w-[7rem] px-3 py-2.5 text-left font-medium">
                  Provider
                </th>
                <th scope="col" className="min-w-[8rem] px-3 py-2.5 text-left font-medium">
                  Model ID
                </th>
                <th scope="col" className="w-[6.5rem] px-2 py-2.5 text-right font-medium">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {models.map((model) => {
                const parts = safeRefParts(model.ref);
                const expanded = expandedRows.has(model.ref);
                const params = getProviderParams(model.spec);

                return (
                  <React.Fragment key={model.ref}>
                    <tr className="border-b border-border/35 transition-colors hover:bg-muted/25">
                      <td className="align-middle px-2 py-3">
                        <button
                          type="button"
                          className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/80 hover:text-foreground"
                          onClick={() => onToggleRow(model.ref)}
                          aria-expanded={expanded}
                          aria-label={expanded ? "Hide details" : "Show details"}
                        >
                          {expanded ? <ChevronDown className="h-4 w-4" aria-hidden /> : <ChevronRight className="h-4 w-4" aria-hidden />}
                        </button>
                      </td>
                      <td className="max-w-[min(24rem,40vw)] align-middle px-3 py-3">
                        <p className="font-mono text-sm font-medium leading-snug text-foreground [overflow-wrap:anywhere]" translate="no">
                          {parts.name || model.ref}
                        </p>
                        <p className="mt-0.5 text-xs text-muted-foreground">Namespace · {parts.namespace || "—"}</p>
                      </td>
                      <td className="align-middle px-3 py-3">
                        <span className="inline-flex max-w-full rounded-md border border-border/60 bg-muted/35 px-2 py-0.5 text-xs font-medium text-muted-foreground">
                          {String(model.spec.provider)}
                        </span>
                      </td>
                      <td className="align-middle px-3 py-3">
                        <span className="text-[15px] leading-snug text-foreground [overflow-wrap:anywhere]" translate="no">
                          {model.spec.model}
                        </span>
                      </td>
                      <td className="align-middle px-2 py-3 text-right">
                        <div className="inline-flex justify-end gap-0.5">
                          <Button
                            data-test={`edit-model-${model.ref}`}
                            variant="ghost"
                            size="icon"
                            className="h-9 w-9 shrink-0 text-muted-foreground hover:text-foreground"
                            onClick={() => onEdit(model)}
                            aria-label={`Edit model ${model.ref}`}
                          >
                            <Pencil className="h-4 w-4" aria-hidden />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-9 w-9 shrink-0 text-muted-foreground hover:text-destructive"
                            onClick={() => onRequestDelete(model)}
                            aria-label={`Delete model ${model.ref}`}
                          >
                            <Trash2 className="h-4 w-4" aria-hidden />
                          </Button>
                        </div>
                      </td>
                    </tr>
                    {expanded ? (
                      <tr className="border-b border-border/35 bg-muted/15">
                        <td colSpan={5} className="px-4 py-4">
                          <dl className="grid gap-4 sm:grid-cols-2">
                            <div>
                              <dt className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Full ref</dt>
                              <dd className="mt-1 font-mono text-sm text-foreground [overflow-wrap:anywhere]">{model.ref}</dd>
                            </div>
                            <div>
                              <dt className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">API key secret</dt>
                              <dd className="mt-1 text-sm text-foreground">{model.spec.apiKeySecret || "—"}</dd>
                            </div>
                            {params != null ? (
                              <div className="sm:col-span-2">
                                <dt className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                                  Provider parameters
                                </dt>
                                <dd className="mt-2">
                                  <pre className="max-h-48 overflow-auto rounded-lg border border-border/60 bg-background/80 p-3 text-left text-xs leading-relaxed text-muted-foreground">
                                    {JSON.stringify(params, null, 2)}
                                  </pre>
                                </dd>
                              </div>
                            ) : null}
                          </dl>
                        </td>
                      </tr>
                    ) : null}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
      )}

      <Dialog open={modelToDelete !== null} onOpenChange={(open) => !open && onDismissDeleteDialog()}>
        <DialogContent className="overscroll-contain">
          <DialogHeader>
            <DialogTitle>Delete Model</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete the model &apos;{modelToDelete?.ref}&apos;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="flex justify-end space-x-2">
            <Button variant="outline" onClick={onDismissDeleteDialog}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => void onConfirmDelete()}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
