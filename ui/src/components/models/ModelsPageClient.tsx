"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Plus } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ErrorState } from "@/components/ErrorState";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { ModelsListSection } from "@/components/models/ModelsListSection";
import { deleteModelConfig, getModelConfigs } from "@/app/actions/modelConfigs";
import { k8sRefUtils } from "@/lib/k8sUtils";
import type { ModelConfig } from "@/types";

export function ModelsPageClient({
  initialModels,
  initialError,
}: {
  initialModels: ModelConfig[];
  initialError: string | null;
}) {
  const router = useRouter();
  const [models, setModels] = useState(initialModels);
  const [error, setError] = useState<string | null>(initialError);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  const [modelToDelete, setModelToDelete] = useState<ModelConfig | null>(null);

  const fetchModels = async () => {
    const response = await getModelConfigs();
    if (response.error) {
      setError(response.error);
      toast.error(response.error);
      return;
    }
    setModels(response.data ?? []);
    setError(null);
  };

  const toggleRow = (modelRef: string) => {
    setExpandedRows((previous) => {
      const next = new Set(previous);
      if (next.has(modelRef)) next.delete(modelRef);
      else next.add(modelRef);
      return next;
    });
  };

  const handleEdit = (model: ModelConfig) => {
    const modelRef = k8sRefUtils.fromRef(model.ref);
    router.push(`/models/new?edit=true&name=${modelRef.name}&namespace=${modelRef.namespace}`);
  };

  const confirmDelete = async () => {
    if (!modelToDelete) return;
    try {
      const response = await deleteModelConfig(modelToDelete.ref);
      if (response.error) throw new Error(response.error || "Failed to delete model");
      toast.success(`Model "${modelToDelete.ref}" deleted successfully`);
      setModelToDelete(null);
      await fetchModels();
    } catch (caughtError) {
      toast.error(caughtError instanceof Error ? caughtError.message : "Failed to delete model");
      setModelToDelete(null);
    }
  };

  if (error) return <ErrorState message={error} />;

  return (
    <AppPageFrame ariaLabelledBy="models-list-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <PageHeader
        titleId="models-list-title"
        title="Models"
        description="Model configs, providers, and credentials that agents use at runtime."
        className="mb-8"
        end={
          <Button onClick={() => router.push("/models/new")} className="w-full sm:w-auto" size="lg">
            <Plus className="mr-2 h-4 w-4" aria-hidden />
            New Model
          </Button>
        }
      />
      <ModelsListSection
        models={models}
        expandedRows={expandedRows}
        onToggleRow={toggleRow}
        onEdit={handleEdit}
        onRequestDelete={setModelToDelete}
        modelToDelete={modelToDelete}
        onDismissDeleteDialog={() => setModelToDelete(null)}
        onConfirmDelete={confirmDelete}
        onNewModel={() => router.push("/models/new")}
      />
    </AppPageFrame>
  );
}
