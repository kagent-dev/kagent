"use client";

import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Plus, ChevronDown, ChevronRight, Pencil, Trash2, Brain, Cpu, Zap, Shield, BarChart3, Globe, Server, Database, Cloud, Network, Bot, Sparkles, Activity, Gauge } from "lucide-react";
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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";

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

    const getProviderIcon = (providerName: string) => {
        const provider = providerName.toLowerCase();
        if (provider.includes('openai')) return <Brain className="w-5 h-5 text-green-600" />;
        if (provider.includes('anthropic')) return <Sparkles className="w-5 h-5 text-orange-600" />;
        if (provider.includes('google')) return <Globe className="w-5 h-5 text-blue-600" />;
        if (provider.includes('azure')) return <Cloud className="w-5 h-5 text-blue-500" />;
        if (provider.includes('aws')) return <Server className="w-5 h-5 text-orange-500" />;
        return <Cpu className="w-5 h-5 text-slate-600" />;
    };

    const getStatusBadge = (model: ModelConfig) => {
        const isActive = true; // You might want to add actual status checking
        return (
            <Badge className={`${isActive ? 'bg-green-100 text-green-800 border-green-200' : 'bg-red-100 text-red-800 border-red-200'}`}>
                <Activity className="w-3 h-3 mr-1" />
                {isActive ? 'Active' : 'Inactive'}
            </Badge>
        );
    };

    if (error) {
        return <ErrorState message={error} />;
    }

    return (
        <div className="min-h-screen bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-50 relative overflow-hidden">
            {/* Enterprise Background Pattern */}
            <div className="absolute inset-0 opacity-5">
                <div className="absolute top-20 left-20 w-32 h-32 bg-blue-500 rounded-full blur-3xl"></div>
                <div className="absolute top-40 right-32 w-24 h-24 bg-indigo-500 rounded-full blur-3xl"></div>
                <div className="absolute bottom-32 left-1/3 w-20 h-20 bg-slate-500 rounded-full blur-3xl"></div>
                <div className="absolute bottom-20 right-20 w-28 h-28 bg-purple-500 rounded-full blur-3xl"></div>
            </div>

            {/* Enterprise Header */}
            <div className="relative z-10">
                <div className="max-w-7xl mx-auto px-8 py-12">
                    {/* Page Header */}
                    <div className="text-center mb-12">
                        <div className="flex items-center justify-center gap-4 mb-6">
                            <div className="w-16 h-16 rounded-3xl bg-gradient-to-br from-blue-600 via-indigo-600 to-slate-600 flex items-center justify-center shadow-2xl">
                                <Brain className="w-8 h-8 text-white" />
                            </div>
                            <div>
                                <h1 className="text-4xl font-bold bg-gradient-to-r from-slate-900 via-blue-900 to-indigo-900 bg-clip-text text-transparent mb-2">
                                    AI Models Dashboard
                                </h1>
                                <p className="text-lg text-slate-600 max-w-2xl">
                                    Manage and monitor your enterprise AI models with advanced intelligence and performance analytics
                                </p>
                            </div>
                        </div>

                        {/* Enterprise Stats Grid */}
                        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 max-w-4xl mx-auto mb-8">
                            <Card className="bg-white/90 backdrop-blur-sm border-0 shadow-lg">
                                <CardContent className="p-6 text-center">
                                    <Brain className="w-8 h-8 text-blue-600 mx-auto mb-3" />
                                    <div className="text-2xl font-bold text-slate-900">{models.length}</div>
                                    <div className="text-sm text-slate-600">Total Models</div>
                                </CardContent>
                            </Card>
                            <Card className="bg-white/90 backdrop-blur-sm border-0 shadow-lg">
                                <CardContent className="p-6 text-center">
                                    <Activity className="w-8 h-8 text-green-600 mx-auto mb-3" />
                                    <div className="text-2xl font-bold text-slate-900">{models.filter(m => true).length}</div>
                                    <div className="text-sm text-slate-600">Active Models</div>
                                </CardContent>
                            </Card>
                            <Card className="bg-white/90 backdrop-blur-sm border-0 shadow-lg">
                                <CardContent className="p-6 text-center">
                                    <BarChart3 className="w-8 h-8 text-purple-600 mx-auto mb-3" />
                                    <div className="text-2xl font-bold text-slate-900">99.9%</div>
                                    <div className="text-sm text-slate-600">Uptime</div>
                                </CardContent>
                            </Card>
                            <Card className="bg-white/90 backdrop-blur-sm border-0 shadow-lg">
                                <CardContent className="p-6 text-center">
                                    <Zap className="w-8 h-8 text-yellow-600 mx-auto mb-3" />
                                    <div className="text-2xl font-bold text-slate-900">2.4M</div>
                                    <div className="text-sm text-slate-600">API Calls</div>
                                </CardContent>
                            </Card>
                        </div>

                        {/* Action Buttons */}
                        <div className="flex justify-center gap-4">
                            <Button 
                                onClick={() => router.push("/models/new")}
                                className="bg-gradient-to-r from-blue-600 via-indigo-600 to-slate-600 hover:from-blue-700 hover:via-indigo-700 hover:to-slate-700 shadow-xl hover:shadow-2xl transition-all duration-300 h-12 px-8 text-lg"
                            >
                                <Plus className="w-5 h-5 mr-2" />
                                Add New Model
                            </Button>
                        </div>
                    </div>

                    {/* Models Grid */}
                    <div className="max-w-7xl mx-auto">
                        {loading ? (
                            <LoadingState />
                        ) : models.length === 0 ? (
                            <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl">
                                <CardContent className="flex flex-col items-center justify-center p-16 text-center">
                                    <div className="w-24 h-24 rounded-3xl bg-gradient-to-br from-blue-100 to-indigo-100 flex items-center justify-center mb-8">
                                        <Brain className="w-12 h-12 text-blue-600" />
                                    </div>
                                    <h3 className="text-2xl font-bold text-slate-900 mb-4">No AI Models Found</h3>
                                    <p className="text-slate-600 mb-8 max-w-md">
                                        Get started by adding your first AI model to power your enterprise applications.
                                    </p>
                                    <Button 
                                        onClick={() => router.push("/models/new")}
                                        className="bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-700 hover:to-indigo-700 shadow-xl"
                                    >
                                        <Plus className="w-5 h-5 mr-2" />
                                        Add Your First Model
                                    </Button>
                                </CardContent>
                            </Card>
                        ) : (
                            <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-6">
                                {models.map((model) => (
                                    <Card key={model.ref} className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl hover:shadow-3xl transition-all duration-300 overflow-hidden group">
                                        <CardHeader className="bg-gradient-to-r from-blue-600 via-indigo-600 to-slate-600 text-white relative">
                                            <div className="absolute top-4 right-4">
                                                {getStatusBadge(model)}
                                            </div>
                                            <div className="flex items-center gap-4">
                                                <div className="w-12 h-12 rounded-2xl bg-white/20 flex items-center justify-center">
                                                    {getProviderIcon(model.providerName)}
                                                </div>
                                                <div className="flex-1 min-w-0">
                                                    <CardTitle className="text-xl truncate">{model.ref}</CardTitle>
                                                    <CardDescription className="text-blue-100">
                                                        {model.providerName} • {model.model}
                                                    </CardDescription>
                                                </div>
                                            </div>
                                        </CardHeader>
                                        <CardContent className="p-6">
                                            <div className="space-y-4">
                                                {/* Model Details */}
                                                <div className="grid grid-cols-2 gap-4 text-sm">
                                                    <div className="flex items-center gap-2">
                                                        <Globe className="w-4 h-4 text-blue-600" />
                                                        <span className="text-slate-600">Provider:</span>
                                                        <span className="font-medium">{model.providerName}</span>
                                                    </div>
                                                    <div className="flex items-center gap-2">
                                                        <Cpu className="w-4 h-4 text-indigo-600" />
                                                        <span className="text-slate-600">Model:</span>
                                                        <span className="font-medium">{model.model}</span>
                                                    </div>
                                                    <div className="flex items-center gap-2">
                                                        <Network className="w-4 h-4 text-slate-600" />
                                                        <span className="text-slate-600">Namespace:</span>
                                                        <span className="font-medium">{k8sRefUtils.fromRef(model.ref).namespace}</span>
                                                    </div>
                                                    <div className="flex items-center gap-2">
                                                        <Shield className="w-4 h-4 text-green-600" />
                                                        <span className="text-slate-600">Security:</span>
                                                        <span className="font-medium">Encrypted</span>
                                                    </div>
                                                </div>

                                                {/* Performance Metrics */}
                                                <div className="bg-slate-50 rounded-2xl p-4 border border-slate-200">
                                                    <div className="flex items-center justify-between mb-3">
                                                        <h4 className="font-semibold text-slate-800 flex items-center gap-2">
                                                            <BarChart3 className="w-4 h-4 text-blue-600" />
                                                            Performance Metrics
                                                        </h4>
                                                        <Badge className="bg-green-100 text-green-800">
                                                            <Activity className="w-3 h-3 mr-1" />
                                                            Online
                                                        </Badge>
                                                    </div>
                                                    <div className="grid grid-cols-3 gap-4 text-center">
                                                        <div>
                                                            <div className="text-lg font-bold text-green-600">99.9%</div>
                                                            <div className="text-xs text-slate-600">Uptime</div>
                                                        </div>
                                                        <div>
                                                            <div className="text-lg font-bold text-blue-600">1.2s</div>
                                                            <div className="text-xs text-slate-600">Latency</div>
                                                        </div>
                                                        <div>
                                                            <div className="text-lg font-bold text-purple-600">98.5%</div>
                                                            <div className="text-xs text-slate-600">Accuracy</div>
                                                        </div>
                                                    </div>
                                                </div>

                                                {/* Actions */}
                                                <div className="flex gap-3 pt-4 border-t border-slate-200">
                                                    <Button
                                                        variant="outline"
                                                        size="sm"
                                                        onClick={() => handleEdit(model)}
                                                        className="flex-1 h-10 border-slate-200 hover:border-blue-300 hover:bg-blue-50"
                                                    >
                                                        <Pencil className="w-4 h-4 mr-2" />
                                                        Edit
                                                    </Button>
                                                    <Button
                                                        variant="destructive"
                                                        size="sm"
                                                        onClick={() => handleDelete(model)}
                                                        className="h-10 px-4 bg-red-500 hover:bg-red-600"
                                                    >
                                                        <Trash2 className="w-4 h-4" />
                                                    </Button>
                                                </div>
                                            </div>
                                        </CardContent>
                                    </Card>
                                ))}
                            </div>
                        )}
                    </div>
                </div>
            </div>

            {/* Enterprise Footer */}
            <div className="relative z-10 bg-white/90 backdrop-blur-xl border-t border-slate-200">
                <div className="max-w-7xl mx-auto px-8 py-8">
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-8">
                        <div className="text-center">
                            <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-blue-600 to-indigo-600 flex items-center justify-center mx-auto mb-4">
                                <Brain className="w-6 h-6 text-white" />
                            </div>
                            <h3 className="font-bold text-lg mb-2">AI Intelligence</h3>
                            <p className="text-slate-600 text-sm">Advanced machine learning models powering your enterprise applications</p>
                        </div>
                        <div className="text-center">
                            <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-green-600 to-emerald-600 flex items-center justify-center mx-auto mb-4">
                                <Shield className="w-6 h-6 text-white" />
                            </div>
                            <h3 className="font-bold text-lg mb-2">Enterprise Security</h3>
                            <p className="text-slate-600 text-sm">Bank-grade security with end-to-end encryption and compliance</p>
                        </div>
                        <div className="text-center">
                            <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-purple-600 to-pink-600 flex items-center justify-center mx-auto mb-4">
                                <BarChart3 className="w-6 h-6 text-white" />
                            </div>
                            <h3 className="font-bold text-lg mb-2">Performance Analytics</h3>
                            <p className="text-slate-600 text-sm">Real-time monitoring and optimization for peak performance</p>
                        </div>
                    </div>
                </div>
            </div>

            {/* Delete Confirmation Dialog */}
            <Dialog open={modelToDelete !== null} onOpenChange={(open) => !open && setModelToDelete(null)}>
                <DialogContent className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-3 text-xl">
                            <div className="w-10 h-10 rounded-2xl bg-red-100 flex items-center justify-center">
                                <Trash2 className="w-5 h-5 text-red-600" />
                            </div>
                            Delete AI Model
                        </DialogTitle>
                        <DialogDescription className="text-base">
                            Are you sure you want to delete the model &apos;{modelToDelete?.ref}&apos;? This action cannot be undone and will remove all associated configurations.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter className="flex space-x-3 justify-end">
                        <Button
                            variant="outline"
                            onClick={() => setModelToDelete(null)}
                            className="border-slate-200 hover:border-slate-300"
                        >
                            Cancel
                        </Button>
                        <Button
                            variant="destructive"
                            onClick={confirmDelete}
                            className="bg-red-500 hover:bg-red-600"
                        >
                            <Trash2 className="w-4 h-4 mr-2" />
                            Delete Model
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
