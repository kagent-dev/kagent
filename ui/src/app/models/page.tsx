"use client";

import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { ModelConfig } from "@/types";
import { getModelConfigs, deleteModelConfig } from "@/app/actions/modelConfigs";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { toast } from "sonner";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { ModelsListSection } from "@/components/models/ModelsListSection";

export default function ModelsPage() {
  const router = useRouter();
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  const [modelToDelete, setModelToDelete] = useState<ModelConfig | null>(null);

  useEffect(() => {
    void fetchModels();
  }, []);

  const fetchModels = async () => {
    try {
      setLoading(true);
      const response = await getModelConfigs();
      if (response.error || !response.data) {
        throw new Error(response.error || "Failed to fetch models");
      }
      setModels(response.data);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to fetch models";
      setError(errorMessage);
      toast.error(errorMessage);
    } finally {
      setLoading(false);
    }
  };

  const toggleRow = (modelRef: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(modelRef)) {
        next.delete(modelRef);
      } else {
        next.add(modelRef);
      }
      return next;
    });
  };

  const handleEdit = (model: ModelConfig) => {
    const modelRef = k8sRefUtils.fromRef(model.ref);
    router.push(`/models/new?edit=true&name=${modelRef.name}&namespace=${modelRef.namespace}`);
  };

  const confirmDelete = async () => {
    if (!modelToDelete) {
      return;
    }

    try {
      const response = await deleteModelConfig(modelToDelete.ref);
      if (response.error) {
        throw new Error(response.error || "Failed to delete model");
      }
      toast.success(`Model "${modelToDelete.ref}" deleted successfully`);
      setModelToDelete(null);
      await fetchModels();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to delete model";
      toast.error(errorMessage);
      setModelToDelete(null);
    }
  };

  if (error) {
    return <ErrorState message={error} />;
  }

  return (
    <AppPageFrame ariaLabelledBy="models-list-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <div>
        <PageHeader
          titleId="models-list-title"
          title="Models"
          description="Model configs, providers, and credentials that agents use at runtime."
          className="mb-8"
          end={
            <Button variant="default" onClick={() => router.push("/models/new")} className="w-full sm:w-auto" size="lg">
              <Plus className="mr-2 h-4 w-4" aria-hidden />
              New Model
            </Button>
          }
        />

        {loading ? (
          <LoadingState />
        ) : (
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
        )}
      </div>
    </AppPageFrame>
  );
}
