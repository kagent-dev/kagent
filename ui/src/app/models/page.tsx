"use client";
import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Plus, ChevronDown, ChevronRight, Pencil, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { ModelConfig } from "@/types";
import { getModelConfigs, deleteModelConfig } from "@/app/actions/modelConfigs";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { toast } from "sonner";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { k8sRefUtils } from "@/lib/k8sUtils";

export default function ModelsPage() {
    const router = useRouter();
    const [models, setModels] = useState<ModelConfig[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
    const [modelToDelete, setModelToDelete] = useState<ModelConfig | null>(null);

    useEffect(() => {
        fetchModels();
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

    const toggleRow = (modelName: string) => {
        const newExpandedRows = new Set(expandedRows);
        if (expandedRows.has(modelName)) {
            newExpandedRows.delete(modelName);
        } else {
            newExpandedRows.add(modelName);
        }
        setExpandedRows(newExpandedRows);
    };

    const handleEdit = (model: ModelConfig) => {
        const modelRef = k8sRefUtils.fromRef(model.ref);
        router.push(`/models/new?edit=true&name=${modelRef.name}&namespace=${modelRef.namespace}`);
    };

    const handleDelete = async (model: ModelConfig) => {
        setModelToDelete(model);
    };

    const confirmDelete = async () => {
        if (!modelToDelete) return;

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
        <div className="min-h-screen p-8">
            <div className="max-w-6xl mx-auto">
                <div className="flex justify-between items-center mb-8">
                    <h1 className="text-2xl font-bold">Models</h1>
                    <Button
                        variant="default"
                        onClick={() => router.push("/models/new")}
                    >
                        <Plus className="h-4 w-4 mr-2" />
                        New Model
                    </Button>
                </div>

                {loading ? (
                    <LoadingState />
                ) : (
                    <div className="space-y-4">
                        {models.map((model) => (
                            <div key={model.ref} className="border rounded-lg overflow-hidden">
                                <div
                                    className="flex items-center justify-between p-4 cursor-pointer hover:bg-secondary/5"
                                    onClick={() => toggleRow(model.ref)}
                                >
                                    <div className="flex items-center space-x-2">
                                        {expandedRows.has(model.ref) ? (
                                            <ChevronDown className="h-4 w-4" />
                                        ) : (
                                            <ChevronRight className="h-4 w-4" />
                                        )}
                                        <span className="font-medium">{model.ref}</span>
                                    </div>
                                    <div className="flex space-x-2">
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={(e) => {
                                                e.stopPropagation();
                                                handleEdit(model);
                                            }}
                                        >
                                            <Pencil className="h-4 w-4" />
                                        </Button>
                                        <Button
                                            variant="destructive"
                                            size="sm"
                                            onClick={(e) => {
                                                e.stopPropagation();
                                                handleDelete(model);
                                            }}
                                        >
                                            <Trash2 className="h-4 w-4" />
                                        </Button>
                                    </div>
                                </div>
                                {expandedRows.has(model.ref) && (
                                    <div className="p-4 border-t bg-secondary/10">
                                        <div className="grid grid-cols-2 gap-4">
                                            <div>
                                                <p className="text-sm font-medium text-muted-foreground">Provider</p>
                                                <p>{model.providerName}</p>
                                            </div>
                                            <div>
                                                <p className="text-sm font-medium text-muted-foreground">Model</p>
                                                <p>{model.model}</p>
                                            </div>
                                            <div>
                                                <p className="text-sm font-medium text-muted-foreground">Namespace</p>
                                                <p>{k8sRefUtils.fromRef(model.ref).namespace}</p>
                                            </div>
                                            <div>
                                                <p className="text-sm font-medium text-muted-foreground">API Key Secret</p>
                                                <p>{model.apiKeySecretRef ? model.apiKeySecretRef : "N/A"}</p>
                                            </div>
                                            {model.modelParams && (
                                                <div className="col-span-2">
                                                    <p className="text-sm font-medium text-muted-foreground">Model Parameters</p>
                                                    <pre className="mt-1 text-sm bg-muted p-2 rounded">
                                                        {JSON.stringify(model.modelParams, null, 2)}
                                                    </pre>
                                                </div>
                                            )}
                                        </div>
                                    </div>
                                )}
                            </div>
                        ))}
                    </div>
                )}


                <Dialog open={modelToDelete !== null} onOpenChange={(open) => !open && setModelToDelete(null)}>
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Delete Model</DialogTitle>
                            <DialogDescription>
                                Are you sure you want to delete the model &apos;{modelToDelete?.ref}&apos;? This action cannot be undone.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter className="flex space-x-2 justify-end">
                            <Button
                                variant="outline"
                                onClick={() => setModelToDelete(null)}
                            >
                                Cancel
                            </Button>
                            <Button
                                variant="destructive"
                                onClick={confirmDelete}
                            >
                                Delete
                            </Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>
            </div>
        </div>
    );
} 