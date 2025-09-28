"use client";

import React, { useState, useEffect, Suspense } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Loader2, Settings2, PlusCircle, Trash2, ShieldAlert, Brain, Sparkles, Building, Users, BarChart3, Shield, Globe, Rocket, Cpu, Network, Database, Cloud, Lock, Key, Server, Container, Zap, Bot, MessageSquare } from "lucide-react";
import { ModelConfig, AgentType } from "@/types";
import { SystemPromptSection } from "@/components/create/SystemPromptSection";
import { ModelSelectionSection } from "@/components/create/ModelSelectionSection";
import { ToolsSection } from "@/components/create/ToolsSection";
import { useRouter, useSearchParams } from "next/navigation";
import { useAgents } from "@/components/AgentsProvider";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import KagentLogo from "@/components/kagent-logo";
import { AgentFormData } from "@/components/AgentsProvider";
import { Tool, EnvVar } from "@/types";
import { toast } from "sonner";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { useAuth } from "@/hooks/useAuth";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";

interface ValidationErrors {
  name?: string;
  namespace?: string;
  description?: string;
  type?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
}

interface AgentPageContentProps {
  isEditMode: boolean;
  agentName: string | null;
  agentNamespace: string | null;
}

const DEFAULT_SYSTEM_PROMPT = `You're a helpful enterprise agent, made by the kagent team.

# Instructions
    - If user question is unclear, ask for clarification before running any tools
    - Always be helpful and professional
    - If you don't know how to answer the question DO NOT make things up, tell the user "Sorry, I don't know how to answer that" and ask them to clarify the question further
    - If you are unable to help, or something goes wrong, refer the user to https://kagent.dev for more information or support.

# Response format:
    - ALWAYS format your response as Markdown
    - Your response will include a summary of actions you took and an explanation of the result
    - If you created any artifacts such as files or resources, you will include those in your response as well`

// Inner component that uses useSearchParams, wrapped in Suspense
function AgentPageContent({ isEditMode, agentName, agentNamespace }: AgentPageContentProps) {
  const router = useRouter();
  const { models, loading, error, createNewAgent, updateAgent, getAgent, validateAgentData } = useAgents();

  type SelectedModelType = Pick<ModelConfig, 'ref' | 'model'>;

  interface FormState {
    name: string;
    namespace: string;
    description: string;
    agentType: AgentType;
    systemPrompt: string;
    selectedModel: SelectedModelType | null;
    selectedModels: SelectedModelType[];
    selectedTools: Tool[];
    byoImage: string;
    byoCmd: string;
    byoArgs: string;
    replicas: string;
    imagePullPolicy: string;
    imagePullSecrets: string[];
    envPairs: { name: string; value?: string; isSecret?: boolean; secretName?: string; secretKey?: string; optional?: boolean }[];
    isSubmitting: boolean;
    isLoading: boolean;
    errors: ValidationErrors;
    enabled: boolean;
  }

  const [state, setState] = useState<FormState>({
    name: "",
    namespace: "kagent",
    description: "",
    agentType: "Declarative",
    systemPrompt: isEditMode ? "" : DEFAULT_SYSTEM_PROMPT,
    selectedModel: null,
    selectedModels: [],
    selectedTools: [],
    byoImage: "",
    byoCmd: "",
    byoArgs: "",
    replicas: "",
    imagePullPolicy: "",
    imagePullSecrets: [""],
    envPairs: [{ name: "", value: "", isSecret: false }],
    isSubmitting: false,
    isLoading: isEditMode,
    errors: {},
    enabled: true,
  });

  // Fetch existing agent data if in edit mode
  useEffect(() => {
    const fetchAgentData = async () => {
      if (isEditMode && agentName && agentNamespace) {
        try {
          setState(prev => ({ ...prev, isLoading: true }));
          const agentResponse = await getAgent(agentName, agentNamespace);

          if (!agentResponse) {
            toast.error("Agent not found");
            setState(prev => ({ ...prev, isLoading: false }));
            return;
          }
          const agent = agentResponse.agent;
          if (agent) {
            try {
              // Populate form with existing agent data
              const baseUpdates: Partial<FormState> = {
                name: agent.metadata.name || "",
                namespace: agent.metadata.namespace || "",
                description: agent.spec?.description || "",
                agentType: agent.spec.type,
              };
              // v1alpha2: read type and split specs
              if (agent.spec.type === "Declarative") {
                setState(prev => ({
                  ...prev,
                  ...baseUpdates,
                  systemPrompt: agent.spec?.declarative?.systemMessage || "",
                  selectedTools: (agent.spec?.declarative?.tools && agentResponse.tools) ? agentResponse.tools : [],
                  selectedModel: agentResponse.modelConfigRef ? { model: agentResponse.model || "default-model-config", ref: agentResponse.modelConfigRef } : null,
                  selectedModels: agentResponse.modelConfigRef ? [{ model: agentResponse.model || "default-model-config", ref: agentResponse.modelConfigRef }] : [],
                  byoImage: "",
                  byoCmd: "",
                  byoArgs: "",
                }));
              } else {
                setState(prev => ({
                  ...prev,
                  ...baseUpdates,
                  systemPrompt: "",
                  selectedModel: null,
                  selectedTools: [],
                  byoImage: agent.spec?.byo?.deployment?.image || "",
                  byoCmd: agent.spec?.byo?.deployment?.cmd || "",
                  byoArgs: (agent.spec?.byo?.deployment?.args || []).join(" "),
                  replicas: agent.spec?.byo?.deployment?.replicas !== undefined ? String(agent.spec?.byo?.deployment?.replicas) : "",
                  imagePullPolicy: agent.spec?.byo?.deployment?.imagePullPolicy || "",
                  imagePullSecrets: (agent.spec?.byo?.deployment?.imagePullSecrets || []).map((s: { name: string }) => s.name).concat((agent.spec?.byo?.deployment?.imagePullSecrets || []).length === 0 ? [""] : []),
                  envPairs: (agent.spec?.byo?.deployment?.env || []).map((e: EnvVar) => (
                    e?.valueFrom?.secretKeyRef
                      ? { name: e.name || "", isSecret: true, secretName: e.valueFrom.secretKeyRef.name || "", secretKey: e.valueFrom.secretKeyRef.key || "", optional: e.valueFrom.secretKeyRef.optional }
                      : { name: e.name || "", value: e.value || "", isSecret: false }
                  )).concat((agent.spec?.byo?.deployment?.env || []).length === 0 ? [{ name: "", value: "", isSecret: false }] : []),
                }));
              }

              // Load current enabled state for this agent
              try {
                const res = await fetch('/api/admin/agents-settings', {
                  headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }
                });
                if (res.ok) {
                  const data = await res.json();
                  const ref = `${agent.metadata.namespace || ''}/${agent.metadata.name}`;
                  const enabledMap = data.enabled || {};
                  const currentEnabled = Object.prototype.hasOwnProperty.call(enabledMap, ref) ? enabledMap[ref] : true;
                  setState(prev => ({ ...prev, enabled: currentEnabled }));
                }
              } catch {
                // ignore, default true
              }

            } catch (extractError) {
              console.error("Error extracting assistant data:", extractError);
              toast.error("Failed to extract agent data");
            }
          } else {
            setState(prev => ({ ...prev, enabled: true }));
            toast.error("Agent not found");
          }
        } catch (error) {
          console.error("Error fetching agent:", error);
          toast.error("Failed to load agent data");
        } finally {
          setState(prev => ({ ...prev, isLoading: false }));
        }
      }
    };

    void fetchAgentData();
  }, [isEditMode, agentName, agentNamespace, getAgent]);

  const validateForm = () => {
    const formData = {
      name: state.name,
      namespace: state.namespace,
      description: state.description,
      type: state.agentType,
      systemPrompt: state.systemPrompt,
      modelName: state.selectedModel?.ref || "",
      tools: state.selectedTools,
      byoImage: state.byoImage,
    };

    const newErrors = validateAgentData(formData);
    setState(prev => ({ ...prev, errors: newErrors }));
    return Object.keys(newErrors).length === 0;
  };

  // Add field-level validation functions
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const validateField = (fieldName: keyof ValidationErrors, value: any) => {
    const formData: Partial<AgentFormData> = {};

    // Set only the field being validated
    switch (fieldName) {
      case 'name': formData.name = value; break;
      case 'namespace': formData.namespace = value; break;
      case 'description': formData.description = value; break;
      case 'type': formData.type = value; break;
      case 'systemPrompt': formData.systemPrompt = value; break;
      case 'model': formData.modelName = value; break;
      case 'tools': formData.tools = value; break;
    }

    const fieldErrors = validateAgentData(formData);

    const valueForField = (fieldErrors as Record<string, string | undefined>)[fieldName as string];
    setState(prev => ({
      ...prev,
      errors: {
        ...prev.errors,
        [fieldName]: valueForField,
      }
    }));
  };

  const handleSaveAgent = async () => {
    if (!validateForm()) {
      return;
    }

    try {

      setState(prev => ({ ...prev, isSubmitting: true }));
      if (state.agentType === "Declarative" && !state.selectedModel) {
        throw new Error("Model is required to create a declarative agent.");
      }

      const agentData = {
        name: state.name,
        namespace: state.namespace,
        description: state.description,
        type: state.agentType,
        systemPrompt: state.systemPrompt,
        modelName: state.selectedModel?.ref || "",
        stream: true,
        tools: state.selectedTools,
        // BYO
        byoImage: state.byoImage,
        byoCmd: state.byoCmd || undefined,
        byoArgs: state.byoArgs ? state.byoArgs.split(/\s+/).filter(Boolean) : undefined,
        replicas: state.replicas ? parseInt(state.replicas, 10) : undefined,
        imagePullPolicy: state.imagePullPolicy || undefined,
        imagePullSecrets: (state.imagePullSecrets || []).filter(n => n.trim()).map(n => ({ name: n.trim() })),
        env: (state.envPairs || [])
          .map<EnvVar | null>(ev => {
            const name = (ev.name || "").trim();
            if (!name) return null;
            if (ev.isSecret) {
              const secName = (ev.secretName || "").trim();
              const secKey = (ev.secretKey || "").trim();
              if (!secName || !secKey) return null;
              return {
                name,
                valueFrom: {
                  secretKeyRef: {
                    name: secName,
                    key: secKey,
                    optional: ev.optional,
                  },
                },
              } as EnvVar;
            }
            return { name, value: ev.value ?? "" } as EnvVar;
          })
          .filter((e): e is EnvVar => e !== null),
      };

      let result;

      if (isEditMode && agentName && agentNamespace) {
        // Update existing agent
        result = await updateAgent(agentData);
      } else {
        // Create new agent
        result = await createNewAgent(agentData);
      }

      if (result.error) {
        throw new Error(result.error);
      }

      // Persist enabled/disabled flag
      try {
        const ref = `${state.namespace}/${state.name}`;
        await fetch('/api/admin/agents-settings', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` },
          body: JSON.stringify({ ref, enabled: state.enabled })
        });
      } catch {
        // ignore errors here; it's non-critical for creation
      }

      router.push(`/agents`);
      return;
    } catch (error) {
      console.error(`Error ${isEditMode ? "updating" : "creating"} agent:`, error);
      const errorMessage = error instanceof Error ? error.message : `Failed to ${isEditMode ? "update" : "create"} agent. Please try again.`;
      toast.error(errorMessage);
      setState(prev => ({ ...prev, isSubmitting: false }));
    }
  };

  const renderPageContent = () => {
    if (state.isSubmitting) {
      return <LoadingState />;
    }

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
                    {isEditMode ? "Edit Enterprise Agent" : "Create Enterprise Agent"}
                  </h1>
                  <p className="text-lg text-slate-600 max-w-2xl">
                    {isEditMode 
                      ? "Modify your enterprise AI agent configuration with advanced settings and capabilities"
                      : "Build powerful enterprise AI agents with advanced intelligence, security, and integration capabilities"
                    }
                  </p>
                </div>
              </div>

              {/* Enterprise Features Grid */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 max-w-4xl mx-auto">
                <div className="bg-gradient-to-br from-blue-50 to-indigo-50 p-4 rounded-2xl border border-blue-100 shadow-sm">
                  <Brain className="w-8 h-8 text-blue-600 mb-2 mx-auto" />
                  <div className="text-sm font-semibold text-blue-900">Advanced AI</div>
                </div>
                <div className="bg-gradient-to-br from-indigo-50 to-purple-50 p-4 rounded-2xl border border-indigo-100 shadow-sm">
                  <Shield className="w-8 h-8 text-indigo-600 mb-2 mx-auto" />
                  <div className="text-sm font-semibold text-indigo-900">Enterprise Security</div>
                </div>
                <div className="bg-gradient-to-br from-slate-50 to-gray-50 p-4 rounded-2xl border border-slate-100 shadow-sm">
                  <Network className="w-8 h-8 text-slate-600 mb-2 mx-auto" />
                  <div className="text-sm font-semibold text-slate-900">System Integration</div>
                </div>
                <div className="bg-gradient-to-br from-green-50 to-emerald-50 p-4 rounded-2xl border border-green-100 shadow-sm">
                  <BarChart3 className="w-8 h-8 text-green-600 mb-2 mx-auto" />
                  <div className="text-sm font-semibold text-green-900">Business Intelligence</div>
                </div>
              </div>
            </div>

            {/* Main Form */}
            <div className="max-w-5xl mx-auto space-y-8">
              {/* Basic Information Card */}
              <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
                <CardHeader className="bg-gradient-to-r from-blue-600 via-indigo-600 to-slate-600 text-white">
                  <CardTitle className="flex items-center gap-3 text-2xl">
                    <Building className="w-6 h-6" />
                    Basic Information
                  </CardTitle>
                  <p className="text-blue-100">Configure the fundamental properties of your enterprise AI agent</p>
                </CardHeader>
                <CardContent className="p-8 space-y-6">
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                      <label className="text-lg font-semibold mb-3 block text-slate-800">Agent Name</label>
                      <p className="text-sm mb-3 text-slate-600">
                        Choose a unique identifier for your enterprise AI agent
                      </p>
                      <Input
                        value={state.name}
                        onChange={(e) => setState(prev => ({ ...prev, name: e.target.value }))}
                        onBlur={() => validateField('name', state.name)}
                        className={`h-12 text-lg ${state.errors.name ? "border-red-500" : "border-slate-200 focus:border-blue-500"}`}
                        placeholder="e.g., enterprise-assistant, data-analyzer"
                        disabled={state.isSubmitting || state.isLoading || isEditMode}
                      />
                      {state.errors.name && <p className="text-red-500 text-sm mt-2">{state.errors.name}</p>}
                    </div>

                    <div>
                      <label className="text-lg font-semibold mb-3 block text-slate-800">Namespace</label>
                      <p className="text-sm mb-3 text-slate-600">
                        Select the organizational namespace for deployment
                      </p>
                      <NamespaceCombobox
                        value={state.namespace}
                        onValueChange={(value) => {
                          setState(prev => ({ ...prev, selectedModel: null, namespace: value }));
                          validateField('namespace', value);
                        }}
                        allowedNames={["kagent"]}
                        disabled={state.isSubmitting || state.isLoading || isEditMode}
                      />
                    </div>
                  </div>

                  <div className="flex items-center gap-4 p-4 bg-slate-50 rounded-2xl border border-slate-200">
                    <Checkbox 
                      id="agent-enabled" 
                      checked={state.enabled} 
                      onCheckedChange={(v) => setState(prev => ({ ...prev, enabled: Boolean(v) }))} 
                      disabled={state.isSubmitting || state.isLoading} 
                    />
                    <div className="flex items-center gap-2">
                      <Label htmlFor="agent-enabled" className="text-lg font-semibold cursor-pointer">Enable Agent</Label>
                      <Badge className="bg-green-100 text-green-800">Active</Badge>
                    </div>
                  </div>

                  <div>
                    <Label className="text-lg font-semibold mb-3 block text-slate-800">Agent Type</Label>
                    <p className="text-sm mb-4 text-slate-600">
                      Choose between declarative agents (model-based) or BYO containerized agents
                    </p>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      <Card className={`cursor-pointer transition-all duration-300 ${
                        state.agentType === "Declarative" 
                          ? "border-blue-500 bg-blue-50 shadow-lg" 
                          : "border-slate-200 hover:border-blue-300"
                      }`} onClick={() => setState(prev => ({ ...prev, agentType: "Declarative" }))}>
                        <CardContent className="p-6 text-center">
                          <Brain className="w-12 h-12 text-blue-600 mx-auto mb-3" />
                          <h3 className="font-bold text-lg mb-2">Declarative Agent</h3>
                          <p className="text-sm text-slate-600">AI model-based agent with natural language processing</p>
                        </CardContent>
                      </Card>
                      <Card className={`cursor-pointer transition-all duration-300 ${
                        state.agentType === "BYO" 
                          ? "border-indigo-500 bg-indigo-50 shadow-lg" 
                          : "border-slate-200 hover:border-indigo-300"
                      }`} onClick={() => setState(prev => ({ ...prev, agentType: "BYO" }))}>
                        <CardContent className="p-6 text-center">
                          <Container className="w-12 h-12 text-indigo-600 mx-auto mb-3" />
                          <h3 className="font-bold text-lg mb-2">BYO Agent</h3>
                          <p className="text-sm text-slate-600">Bring your own containerized AI agent</p>
                        </CardContent>
                      </Card>
                    </div>
                  </div>

                  <div>
                    <label className="text-lg font-semibold mb-3 block text-slate-800">Description</label>
                    <p className="text-sm mb-3 text-slate-600">
                      Describe the purpose and capabilities of your enterprise agent
                    </p>
                    <Textarea
                      value={state.description}
                      onChange={(e) => setState(prev => ({ ...prev, description: e.target.value }))}
                      onBlur={() => validateField('description', state.description)}
                      className={`min-h-[120px] text-lg ${state.errors.description ? "border-red-500" : "border-slate-200 focus:border-blue-500"}`}
                      placeholder="Describe your agent's purpose, capabilities, and use cases..."
                      disabled={state.isSubmitting || state.isLoading}
                    />
                    {state.errors.description && <p className="text-red-500 text-sm mt-2">{state.errors.description}</p>}
                  </div>
                </CardContent>
              </Card>

              {/* AI Configuration Card */}
              {state.agentType === "Declarative" && (
                <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
                  <CardHeader className="bg-gradient-to-r from-indigo-600 via-purple-600 to-pink-600 text-white">
                    <CardTitle className="flex items-center gap-3 text-2xl">
                      <Brain className="w-6 h-6" />
                      AI Configuration
                    </CardTitle>
                    <p className="text-indigo-100">Configure the intelligence and capabilities of your AI agent</p>
                  </CardHeader>
                  <CardContent className="p-8 space-y-8">
                    <div>
                      <div className="flex items-center gap-3 mb-4">
                        <MessageSquare className="w-6 h-6 text-indigo-600" />
                        <Label className="text-xl font-bold text-slate-800">System Prompt</Label>
                      </div>
                      <p className="text-sm mb-4 text-slate-600">
                        Define the behavior, personality, and capabilities of your AI agent
                      </p>
                      <div className="bg-slate-50 rounded-2xl p-6 border border-slate-200">
                        <Textarea
                          value={state.systemPrompt}
                          onChange={(e) => setState(prev => ({ ...prev, systemPrompt: e.target.value }))}
                          onBlur={() => validateField('systemPrompt', state.systemPrompt)}
                          className={`min-h-[200px] text-base border-slate-200 focus:border-indigo-500 ${state.errors.systemPrompt ? "border-red-500" : ""}`}
                          placeholder="Define your agent's behavior, instructions, and capabilities..."
                          disabled={state.isSubmitting || state.isLoading}
                        />
                        {state.errors.systemPrompt && <p className="text-red-500 text-sm mt-2">{state.errors.systemPrompt}</p>}
                      </div>
                    </div>

                    <div>
                      <div className="flex items-center gap-3 mb-4">
                        <Cpu className="w-6 h-6 text-purple-600" />
                        <Label className="text-xl font-bold text-slate-800">AI Model</Label>
                      </div>
                      <p className="text-sm mb-4 text-slate-600">
                        Select the foundation model that powers your agent's intelligence
                      </p>
                      <ModelSelectionSection 
                        allModels={models} 
                        selectedModel={state.selectedModel}
                        setSelectedModel={(model) => {
                          setState(prev => ({ ...prev, selectedModel: model as Pick<ModelConfig, 'ref' | 'model'> | null, selectedModels: model ? [model as SelectedModelType] : [] }));
                        }}
                        multi
                        selectedModels={state.selectedModels}
                        setSelectedModels={(modelsArr) => {
                          const first = modelsArr[0] || null;
                          setState(prev => ({ ...prev, selectedModels: modelsArr as SelectedModelType[], selectedModel: first as SelectedModelType | null }));
                          if (first?.ref) {
                            validateField('model', first.ref);
                          }
                        }}
                        error={state.errors.model} 
                        isSubmitting={state.isSubmitting || state.isLoading} 
                        onChange={(modelRef) => validateField('model', modelRef)}
                        agentNamespace={state.namespace}
                      />
                    </div>
                  </CardContent>
                </Card>
              )}

              {/* Container Configuration Card */}
              {state.agentType === "BYO" && (
                <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
                  <CardHeader className="bg-gradient-to-r from-slate-600 via-gray-600 to-blue-600 text-white">
                    <CardTitle className="flex items-center gap-3 text-2xl">
                      <Container className="w-6 h-6" />
                      Container Configuration
                    </CardTitle>
                    <p className="text-slate-100">Configure your custom containerized AI agent</p>
                  </CardHeader>
                  <CardContent className="p-8 space-y-6">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                      <div>
                        <Label className="text-lg font-semibold mb-3 block text-slate-800">Container Image</Label>
                        <p className="text-sm mb-3 text-slate-600">Docker image for your custom agent</p>
                        <Input
                          value={state.byoImage}
                          onChange={(e) => setState(prev => ({ ...prev, byoImage: e.target.value }))}
                          onBlur={() => validateField('model', state.byoImage)}
                          className="h-12 text-lg"
                          placeholder="ghcr.io/your-org/agent:latest"
                          disabled={state.isSubmitting || state.isLoading}
                        />
                        {state.errors.model && <p className="text-red-500 text-sm mt-2">{state.errors.model}</p>}
                      </div>

                      <div>
                        <Label className="text-lg font-semibold mb-3 block text-slate-800">Command</Label>
                        <p className="text-sm mb-3 text-slate-600">Entry point command (optional)</p>
                        <Input
                          value={state.byoCmd}
                          onChange={(e) => setState(prev => ({ ...prev, byoCmd: e.target.value }))}
                          className="h-12 text-lg"
                          placeholder="/app/start"
                          disabled={state.isSubmitting || state.isLoading}
                        />
                      </div>
                    </div>

                    <div>
                      <Label className="text-lg font-semibold mb-3 block text-slate-800">Arguments</Label>
                      <p className="text-sm mb-3 text-slate-600">Space-separated command arguments</p>
                      <Input
                        value={state.byoArgs}
                        onChange={(e) => setState(prev => ({ ...prev, byoArgs: e.target.value }))}
                        className="h-12 text-lg"
                        placeholder="--port 8080 --workers 4"
                        disabled={state.isSubmitting || state.isLoading}
                      />
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                      <div>
                        <Label className="text-lg font-semibold mb-3 block text-slate-800">Replicas</Label>
                        <Input
                          type="number"
                          value={state.replicas}
                          onChange={(e) => setState(prev => ({ ...prev, replicas: e.target.value }))}
                          className="h-12 text-lg"
                          placeholder="3"
                          disabled={state.isSubmitting || state.isLoading}
                        />
                      </div>

                      <div>
                        <Label className="text-lg font-semibold mb-3 block text-slate-800">Image Pull Policy</Label>
                        <Select
                          value={state.imagePullPolicy}
                          onValueChange={(val) => setState(prev => ({ ...prev, imagePullPolicy: val }))}
                          disabled={state.isSubmitting || state.isLoading}
                        >
                          <SelectTrigger className="h-12 text-lg">
                            <SelectValue placeholder="Select policy" />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="Always">Always</SelectItem>
                            <SelectItem value="IfNotPresent">IfNotPresent</SelectItem>
                            <SelectItem value="Never">Never</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              )}

              {/* Tools & Integration Card */}
              {state.agentType === "Declarative" && (
                <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
                  <CardHeader className="bg-gradient-to-r from-green-600 via-emerald-600 to-teal-600 text-white">
                    <CardTitle className="flex items-center gap-3 text-2xl">
                      <Zap className="w-6 h-6" />
                      Tools & Integration
                    </CardTitle>
                    <p className="text-green-100">Connect your agent with powerful tools and external services</p>
                  </CardHeader>
                  <CardContent className="p-8">
                    <ToolsSection 
                      selectedTools={state.selectedTools} 
                      setSelectedTools={(tools) => setState(prev => ({ ...prev, selectedTools: tools }))} 
                      isSubmitting={state.isSubmitting || state.isLoading} 
                      onBlur={() => validateField('tools', state.selectedTools)}
                      currentAgentName={state.name}
                    />
                  </CardContent>
                </Card>
              )}

              {/* Environment Variables Card */}
              {(state.agentType === "BYO" || (state.agentType === "Declarative" && state.selectedTools.length > 0)) && (
                <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
                  <CardHeader className="bg-gradient-to-r from-purple-600 via-pink-600 to-rose-600 text-white">
                    <CardTitle className="flex items-center gap-3 text-2xl">
                      <Key className="w-6 h-6" />
                      Environment & Secrets
                    </CardTitle>
                    <p className="text-purple-100">Configure environment variables and secure secrets for your agent</p>
                  </CardHeader>
                  <CardContent className="p-8 space-y-6">
                    <div className="space-y-4">
                      <Label className="text-lg font-bold text-slate-800">Environment Variables</Label>
                      {(state.envPairs || []).map((pair, index) => (
                        <Card key={index} className="border border-slate-200 shadow-sm">
                          <CardContent className="p-6">
                            <div className="flex items-center gap-4 mb-4">
                              <Input 
                                placeholder="Variable name (e.g., API_KEY)" 
                                value={pair.name} 
                                onChange={(e) => {
                                  const updated = [...state.envPairs];
                                  updated[index] = { ...updated[index], name: e.target.value };
                                  setState(prev => ({ ...prev, envPairs: updated }));
                                }} 
                                className="flex-1 h-12 text-base" 
                                disabled={state.isSubmitting || state.isLoading} 
                              />
                              <div className="flex items-center gap-2">
                                <Checkbox 
                                  id={`env-secret-${index}`} 
                                  checked={!!pair.isSecret} 
                                  onCheckedChange={(checked) => {
                                    const updated = [...state.envPairs];
                                    updated[index] = { ...updated[index], isSecret: !!checked };
                                    setState(prev => ({ ...prev, envPairs: updated }));
                                  }} 
                                />
                                <Label htmlFor={`env-secret-${index}`} className="text-sm font-medium">Secret</Label>
                              </div>
                              <Button 
                                variant="ghost" 
                                size="sm" 
                                onClick={() => setState(prev => ({ ...prev, envPairs: prev.envPairs.filter((_, i) => i !== index) }))} 
                                disabled={(state.envPairs || []).length === 1} 
                                className="p-2"
                              >
                                <Trash2 className="h-4 w-4 text-red-500" />
                              </Button>
                            </div>
                            {!pair.isSecret ? (
                              <Input 
                                placeholder="Value" 
                                value={pair.value ?? ""} 
                                onChange={(e) => {
                                  const updated = [...state.envPairs];
                                  updated[index] = { ...updated[index], value: e.target.value };
                                  setState(prev => ({ ...prev, envPairs: updated }));
                                }} 
                                className="h-12 text-base"
                                disabled={state.isSubmitting || state.isLoading} 
                              />
                            ) : (
                              <div className="grid grid-cols-3 gap-4">
                                <Input 
                                  placeholder="Secret name" 
                                  value={pair.secretName ?? ""} 
                                  onChange={(e) => {
                                    const updated = [...state.envPairs];
                                    updated[index] = { ...updated[index], secretName: e.target.value };
                                    setState(prev => ({ ...prev, envPairs: updated }));
                                  }} 
                                  className="h-12 text-base"
                                  disabled={state.isSubmitting || state.isLoading} 
                                />
                                <Input 
                                  placeholder="Secret key" 
                                  value={pair.secretKey ?? ""} 
                                  onChange={(e) => {
                                    const updated = [...state.envPairs];
                                    updated[index] = { ...updated[index], secretKey: e.target.value };
                                    setState(prev => ({ ...prev, envPairs: updated }));
                                  }} 
                                  className="h-12 text-base"
                                  disabled={state.isSubmitting || state.isLoading} 
                                />
                                <div className="flex items-center gap-2">
                                  <Checkbox 
                                    id={`env-optional-${index}`} 
                                    checked={!!pair.optional} 
                                    onCheckedChange={(checked) => {
                                      const updated = [...state.envPairs];
                                      updated[index] = { ...updated[index], optional: !!checked };
                                      setState(prev => ({ ...prev, envPairs: updated }));
                                    }} 
                                  />
                                  <Label htmlFor={`env-optional-${index}`} className="text-sm">Optional</Label>
                                </div>
                              </div>
                            )}
                          </CardContent>
                        </Card>
                      ))}
                      <Button 
                        variant="outline" 
                        size="lg" 
                        onClick={() => setState(prev => ({ ...prev, envPairs: [...prev.envPairs, { name: "", value: "", isSecret: false }] }))} 
                        className="w-full h-12 text-lg border-2 border-dashed border-slate-300 hover:border-blue-400 hover:bg-blue-50"
                      >
                        <PlusCircle className="h-5 w-5 mr-2" />
                        Add Environment Variable
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              )}

              {/* Action Buttons */}
              <div className="flex justify-center pt-8">
                <div className="flex gap-4">
                  <Button 
                    variant="outline" 
                    onClick={() => router.push('/agents')}
                    className="h-14 px-8 text-lg border-2 border-slate-300 hover:border-slate-400"
                    disabled={state.isSubmitting}
                  >
                    Cancel
                  </Button>
                  <Button 
                    className="h-14 px-12 text-lg bg-gradient-to-r from-blue-600 via-indigo-600 to-slate-600 hover:from-blue-700 hover:via-indigo-700 hover:to-slate-700 shadow-xl hover:shadow-2xl transition-all duration-300"
                    onClick={handleSaveAgent} 
                    disabled={state.isSubmitting || state.isLoading}
                  >
                    {state.isSubmitting ? (
                      <>
                        <Loader2 className="h-5 w-5 mr-3 animate-spin" />
                        {isEditMode ? "Updating Agent..." : "Creating Agent..."}
                      </>
                    ) : isEditMode ? (
                      <>
                        <Brain className="h-5 w-5 mr-3" />
                        Update Enterprise Agent
                      </>
                    ) : (
                      <>
                        <Sparkles className="h-5 w-5 mr-3" />
                        Create Enterprise Agent
                      </>
                    )}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  };

  return (
    <>
      {(loading || state.isLoading) && <LoadingState />}
      {renderPageContent()}
    </>
  );
}

// Main component that wraps the content in a Suspense boundary
export default function AgentPage() {
  const searchParams = useSearchParams();
  const agentName = searchParams.get('name');
  const agentNamespace = searchParams.get('namespace');
  const isEditMode = !!agentName && !!agentNamespace;
  const { user, isAuthenticated, isLoading } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (!isLoading && (!isAuthenticated || user?.role !== 'admin')) {
      toast.error('You do not have permission to access this page');
      router.push('/agents');
    }
  }, [isLoading, isAuthenticated, user, router]);

  if (isLoading) {
    return <LoadingState />;
  }

  if (!isAuthenticated || user?.role !== 'admin') {
    return (
      <div className="flex flex-col items-center justify-center min-h-screen p-4 bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-50">
        <div className="text-center space-y-6 max-w-md">
          <div className="w-20 h-20 rounded-3xl bg-gradient-to-br from-red-500 to-pink-500 flex items-center justify-center mx-auto shadow-2xl">
            <Shield className="w-10 h-10 text-white" />
          </div>
          <div>
            <h2 className="text-3xl font-bold bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent mb-3">
              Access Restricted
            </h2>
            <p className="text-slate-600 text-lg leading-relaxed">
              Enterprise agent creation requires administrator privileges. Please contact your system administrator for access.
            </p>
          </div>
          <Button 
            onClick={() => router.push('/agents')}
            className="bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-700 hover:to-indigo-700 shadow-xl"
          >
            <Building className="w-5 h-5 mr-2" />
            Back to Enterprise Agents
          </Button>
        </div>
      </div>
    );
  }

  return (
    <Suspense fallback={<LoadingState />}>
      <AgentPageContent 
        isEditMode={isEditMode} 
        agentName={agentName}
        agentNamespace={agentNamespace}
      />
    </Suspense>
  );
}
