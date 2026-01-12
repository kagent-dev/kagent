"use client";
import React, { useState, useEffect, Suspense } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Loader2, Settings2, PlusCircle, Trash2 } from "lucide-react";
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
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

interface ValidationErrors {
  name?: string;
  namespace?: string;
  description?: string;
  type?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
  skills?: string;
}

interface AgentPageContentProps {
  isEditMode: boolean;
  agentName: string | null;
  agentNamespace: string | null;
}

const DEFAULT_SYSTEM_PROMPT = `You're a helpful agent, made by the kagent team.

# Instructions
    - If user question is unclear, ask for clarification before running any tools
    - Always be helpful and friendly
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
    selectedTools: Tool[];
    skillRefs: string[];
    byoImage: string;
    byoCmd: string;
    byoArgs: string;
    replicas: string;
    imagePullPolicy: string;
    imagePullSecrets: string[];
    envPairs: { name: string; value?: string; isSecret?: boolean; secretName?: string; secretKey?: string; optional?: boolean }[];
    stream: boolean;
    isSubmitting: boolean;
    isLoading: boolean;
    errors: ValidationErrors;
  }

  const [state, setState] = useState<FormState>({
    name: "",
    namespace: "default",
    description: "",
    agentType: "Declarative",
    systemPrompt: isEditMode ? "" : DEFAULT_SYSTEM_PROMPT,
    selectedModel: null,
    selectedTools: [],
    skillRefs: [""],
    byoImage: "",
    byoCmd: "",
    byoArgs: "",
    replicas: "",
    imagePullPolicy: "",
    imagePullSecrets: [""],
    envPairs: [{ name: "", value: "", isSecret: false }],
    stream: false,
    isSubmitting: false,
    isLoading: isEditMode,
    errors: {},
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
                  skillRefs: (agent.spec?.skills?.refs && agent.spec.skills.refs.length > 0) ? agent.spec.skills.refs : [""],
                  stream: agent.spec?.declarative?.stream ?? false,
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

            } catch (extractError) {
              console.error("Error extracting assistant data:", extractError);
              toast.error("Failed to extract agent data");
            }
          } else {
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

  const isValidContainerImage = (image: string): boolean => {
    if (!image.trim()) return false;
    // Basic regex for container image format: [registry/]repository[:tag|@digest]
    const imageRegex = /^(?:(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}(?::\d+)?\/)?[A-Za-z0-9][A-Za-z0-9._-]*(?:\/[A-Za-z0-9][A-Za-z0-9._-]*)*(?::[A-Za-z0-9][A-Za-z0-9._-]*)?(?:@sha256:[a-f0-9]{64})?$/i;
    return imageRegex.test(image.trim());
  };

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

    if (state.agentType === "Declarative" && state.skillRefs && state.skillRefs.length > 0) {
      // Filter out empty/whitespace entries first - if all are empty, treat as "no skills"
      const nonEmptyRefs = state.skillRefs.filter(ref => ref.trim());
      
      // Only validate if there are actual skill references
      if (nonEmptyRefs.length > 0) {
        // Check for invalid image formats
        const invalidRefs = nonEmptyRefs.filter(ref => !isValidContainerImage(ref));
        if (invalidRefs.length > 0) {
          newErrors.skills = `Invalid container image format: ${invalidRefs[0]}`;
        } else {
          // Check for duplicates (case-insensitive, trimmed)
          const trimmedRefs = nonEmptyRefs.map(ref => ref.trim().toLowerCase());
          const duplicates = trimmedRefs.filter((ref, index) => trimmedRefs.indexOf(ref) !== index);
          if (duplicates.length > 0) {
            // Find the first duplicate in the original array for error message
            const dupIndex = trimmedRefs.findIndex((ref, idx) => trimmedRefs.indexOf(ref) !== idx);
            newErrors.skills = `Duplicate skill detected: ${nonEmptyRefs[dupIndex]}`;
          }
        }
      }
      // If all refs are empty/whitespace, that's fine - no skills will be included
    }

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
        stream: state.stream,
        tools: state.selectedTools,
        skillRefs: state.agentType === "Declarative" ? (state.skillRefs || []).filter(ref => ref.trim()) : undefined,
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
      <div className="min-h-screen p-8">
        <div className="max-w-6xl mx-auto">
          <h1 className="text-2xl font-bold mb-8">{isEditMode ? "Edit Agent" : "Create New Agent"}</h1>

          <div className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-xl font-bold">
                  <KagentLogo className="h-5 w-5" />
                  Basic Information
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="text-base mb-2 block font-bold">Agent Name</label>
                  <p className="text-xs mb-2 block text-muted-foreground">
                    This is the name of the agent that will be displayed in the UI and used to identify the agent.
                  </p>
                  <Input
                    value={state.name}
                    onChange={(e) => setState(prev => ({ ...prev, name: e.target.value }))}
                    onBlur={() => validateField('name', state.name)}
                    className={`${state.errors.name ? "border-red-500" : ""}`}
                    placeholder="Enter agent name..."
                    disabled={state.isSubmitting || state.isLoading || isEditMode}
                  />
                  {state.errors.name && <p className="text-red-500 text-sm mt-1">{state.errors.name}</p>}
                </div>

                <div>
                  <label className="text-base mb-2 block font-bold">Agent Namespace</label>
                  <p className="text-xs mb-2 block text-muted-foreground">
                    This is the namespace of the agent that will be displayed in the UI and used to identify the agent.
                  </p>
                  <NamespaceCombobox
                    value={state.namespace}
                    onValueChange={(value) => {
                      setState(prev => ({ ...prev, selectedModel: null, namespace: value }));
                      validateField('namespace', value);
                    }}
                    disabled={state.isSubmitting || state.isLoading || isEditMode}
                  />
                </div>

                <div>
                  <Label className="text-base mb-2 block font-bold">Agent Type</Label>
                  <p className="text-xs mb-2 block text-muted-foreground">
                    Choose declarative (uses a model) or BYO (bring your own containerized agent).
                  </p>
                  <Select
                    value={state.agentType}
                    onValueChange={(val) => {
                      setState(prev => ({ ...prev, agentType: val as AgentType }));
                      validateField('type', val);
                    }}
                    disabled={state.isSubmitting || state.isLoading}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select agent type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="Declarative">Declarative</SelectItem>
                      <SelectItem value="BYO">BYO</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                <div>
                  <label className="text-sm mb-2 block">Description</label>
                  <p className="text-xs mb-2 block text-muted-foreground">
                    This is a description of the agent. It&apos;s for your reference only and it&apos;s not going to be used by the agent.
                  </p>
                  <Textarea
                    value={state.description}
                    onChange={(e) => setState(prev => ({ ...prev, description: e.target.value }))}
                    onBlur={() => validateField('description', state.description)}
                    className={`min-h-[100px] ${state.errors.description ? "border-red-500" : ""}`}
                    placeholder="Describe your agent. This is for your reference only and it's not going to be used by the agent."
                    disabled={state.isSubmitting || state.isLoading}
                  />
                  {state.errors.description && <p className="text-red-500 text-sm mt-1">{state.errors.description}</p>}
                </div>

                {state.agentType === "Declarative" && (
                  <>
                    <SystemPromptSection
                      value={state.systemPrompt}
                      onChange={(e) => setState(prev => ({ ...prev, systemPrompt: e.target.value }))}
                      onBlur={() => validateField('systemPrompt', state.systemPrompt)}
                      error={state.errors.systemPrompt}
                      disabled={state.isSubmitting || state.isLoading}
                    />

                    <ModelSelectionSection
                      allModels={models}
                      selectedModel={state.selectedModel}
                      setSelectedModel={(model) => {
                        setState(prev => ({ ...prev, selectedModel: model as Pick<ModelConfig, 'ref' | 'model'> | null }));
                      }}
                      error={state.errors.model}
                      isSubmitting={state.isSubmitting || state.isLoading}
                      onChange={(modelRef) => validateField('model', modelRef)}
                      agentNamespace={state.namespace}
                    />

                    <div className="flex items-center space-x-3 pt-2">
                      <Checkbox
                        id="stream-toggle"
                        checked={state.stream}
                        onCheckedChange={(checked) => setState(prev => ({ ...prev, stream: !!checked }))}
                        disabled={state.isSubmitting || state.isLoading}
                      />
                      <div>
                        <Label htmlFor="stream-toggle" className="text-sm font-medium">Enable LLM response streaming</Label>
                        <p className="text-xs text-muted-foreground">Stream responses from the model in real-time (experimental)</p>
                      </div>
                    </div>
                  </>
                )}
                {state.agentType === "BYO" && (
                  <div className="space-y-4">
                    <div>
                      <Label className="text-sm mb-2 block">Container image</Label>
                      <Input
                        value={state.byoImage}
                        onChange={(e) => setState(prev => ({ ...prev, byoImage: e.target.value }))}
                        onBlur={() => validateField('model', state.byoImage)}
                        placeholder="e.g. ghcr.io/you/agent:latest"
                        disabled={state.isSubmitting || state.isLoading}
                      />
                      {state.errors.model && <p className="text-red-500 text-sm mt-1">{state.errors.model}</p>}
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <Label className="text-sm mb-2 block">Command (optional)</Label>
                        <Input
                          value={state.byoCmd}
                          onChange={(e) => setState(prev => ({ ...prev, byoCmd: e.target.value }))}
                          placeholder="/app/start"
                          disabled={state.isSubmitting || state.isLoading}
                        />
                      </div>
                      <div>
                        <Label className="text-sm mb-2 block">Args (space-separated)</Label>
                        <Input
                          value={state.byoArgs}
                          onChange={(e) => setState(prev => ({ ...prev, byoArgs: e.target.value }))}
                          placeholder="--port 8080 --flag"
                          disabled={state.isSubmitting || state.isLoading}
                        />
                      </div>
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <Label className="text-sm mb-2 block">Replicas</Label>
                        <Input
                          type="number"
                          value={state.replicas}
                          onChange={(e) => setState(prev => ({ ...prev, replicas: e.target.value }))}
                          placeholder="e.g. 1"
                          disabled={state.isSubmitting || state.isLoading}
                        />
                      </div>
                      <div>
                        <Label className="text-sm mb-2 block">Image Pull Policy</Label>
                        <Select
                          value={state.imagePullPolicy}
                          onValueChange={(val) => setState(prev => ({ ...prev, imagePullPolicy: val }))}
                          disabled={state.isSubmitting || state.isLoading}
                        >
                          <SelectTrigger>
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

                    <div className="space-y-2">
                      <Label className="text-sm">Image Pull Secrets</Label>
                      {(state.imagePullSecrets || []).map((name, idx) => (
                        <div key={idx} className="flex gap-2 items-center">
                          <Input
                            placeholder="Secret name"
                            value={name}
                            onChange={(e) => {
                              const copy = [...state.imagePullSecrets];
                              copy[idx] = e.target.value;
                              setState(prev => ({ ...prev, imagePullSecrets: copy }));
                            }}
                            disabled={state.isSubmitting || state.isLoading}
                          />
                          <Button variant="outline" onClick={() => setState(prev => ({ ...prev, imagePullSecrets: [...prev.imagePullSecrets, ""] }))}>Add</Button>
                          <Button variant="ghost" onClick={() => setState(prev => ({ ...prev, imagePullSecrets: prev.imagePullSecrets.filter((_, i) => i !== idx) }))} disabled={(state.imagePullSecrets || []).length <= 1}>Remove</Button>
                        </div>
                      ))}
                    </div>

                    <div className="space-y-2">
                      <Label className="text-sm">Environment Variables</Label>
                      {(state.envPairs || []).map((pair, index) => (
                        <div key={index} className="flex flex-col gap-2 border rounded-md p-2">
                          <div className="flex items-center gap-2">
                            <Input placeholder="Name (e.g., GOOGLE_API_KEY)" value={pair.name} onChange={(e) => {
                              const updated = [...state.envPairs];
                              updated[index] = { ...updated[index], name: e.target.value };
                              setState(prev => ({ ...prev, envPairs: updated }));
                            }} className="flex-1" disabled={state.isSubmitting || state.isLoading} />
                            <div className="flex items-center gap-2">
                              <Checkbox id={`env-secret-${index}`} checked={!!pair.isSecret} onCheckedChange={(checked) => {
                                const updated = [...state.envPairs];
                                updated[index] = { ...updated[index], isSecret: !!checked };
                                setState(prev => ({ ...prev, envPairs: updated }));
                              }} />
                              <Label htmlFor={`env-secret-${index}`} className="text-xs">From Secret</Label>
                            </div>
                            <Button variant="ghost" size="sm" onClick={() => setState(prev => ({ ...prev, envPairs: prev.envPairs.filter((_, i) => i !== index) }))} disabled={(state.envPairs || []).length === 1} className="p-1">
                              <Trash2 className="h-4 w-4 text-red-500" />
                            </Button>
                          </div>
                          {!pair.isSecret ? (
                            <Input placeholder="Value" value={pair.value ?? ""} onChange={(e) => {
                              const updated = [...state.envPairs];
                              updated[index] = { ...updated[index], value: e.target.value };
                              setState(prev => ({ ...prev, envPairs: updated }));
                            }} disabled={state.isSubmitting || state.isLoading} />
                          ) : (
                            <div className="grid grid-cols-3 gap-2">
                              <Input placeholder="Secret name" value={pair.secretName ?? ""} onChange={(e) => {
                                const updated = [...state.envPairs];
                                updated[index] = { ...updated[index], secretName: e.target.value };
                                setState(prev => ({ ...prev, envPairs: updated }));
                              }} disabled={state.isSubmitting || state.isLoading} />
                              <Input placeholder="Secret key" value={pair.secretKey ?? ""} onChange={(e) => {
                                const updated = [...state.envPairs];
                                updated[index] = { ...updated[index], secretKey: e.target.value };
                                setState(prev => ({ ...prev, envPairs: updated }));
                              }} disabled={state.isSubmitting || state.isLoading} />
                              <div className="flex items-center gap-2">
                                <Checkbox id={`env-optional-${index}`} checked={!!pair.optional} onCheckedChange={(checked) => {
                                  const updated = [...state.envPairs];
                                  updated[index] = { ...updated[index], optional: !!checked };
                                  setState(prev => ({ ...prev, envPairs: updated }));
                                }} />
                                <Label htmlFor={`env-optional-${index}`} className="text-xs">Optional</Label>
                              </div>
                            </div>
                          )}
                        </div>
                      ))}
                      <Button variant="outline" size="sm" onClick={() => setState(prev => ({ ...prev, envPairs: [...prev.envPairs, { name: "", value: "", isSecret: false }] }))} className="mt-2 w-full">
                        <PlusCircle className="h-4 w-4 mr-2" />
                        Add Environment Variable
                      </Button>
                    </div>


                  </div>
                )}
              </CardContent>
            </Card>
            {state.agentType === "Declarative" && (
              <>
                <Card>
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <Settings2 className="h-5 w-5 text-yellow-500" />
                      Tools & Agents
                    </CardTitle>
                  </CardHeader>
                  <CardContent>
                    <ToolsSection
                      selectedTools={state.selectedTools}
                      setSelectedTools={(tools) => setState(prev => ({ ...prev, selectedTools: tools }))}
                      isSubmitting={state.isSubmitting || state.isLoading}
                      onBlur={() => validateField('tools', state.selectedTools)}
                      currentAgentName={state.name}
                    />
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <Settings2 className="h-5 w-5 text-blue-500" />
                      Skills
                    </CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-4">
                      <div>
                        <Label className="text-sm mb-2 block font-semibold">Skill Container Images</Label>
                        <p className="text-xs mb-2 block text-muted-foreground">
                          Add skills container images. Each skill will be pulled and mounted for your agent to use.
                        </p>
                        <div className="space-y-2">
                          {(state.skillRefs || []).map((ref, idx) => {
                            const isDuplicate = ref.trim() && state.skillRefs.filter(r => r.trim() === ref.trim()).length > 1;
                            const isInvalid = ref.trim() && !isValidContainerImage(ref);
                            const hasError = isDuplicate || isInvalid;

                            return (
                              <div key={idx} className="space-y-1">
                                <div className="flex gap-2 items-center">
                                  <div className="flex-1">
                                    <Input
                                      placeholder={"ghcr.io/example/python-skill:v1.0.0"}
                                      value={ref}
                                      onChange={(e) => {
                                        const copy = [...state.skillRefs];
                                        copy[idx] = e.target.value;
                                        setState(prev => ({ ...prev, skillRefs: copy, errors: { ...prev.errors, skills: undefined } }));
                                      }}
                                      disabled={state.isSubmitting || state.isLoading}
                                      className={hasError ? "border-red-500" : ""}
                                    />
                                    {isDuplicate && (
                                      <p className="text-xs text-red-500 mt-1">⚠️ This skill is already added</p>
                                    )}
                                    {isInvalid && (
                                      <p className="text-xs text-red-500 mt-1">⚠️ Invalid image format (expected: registry/repository:tag)</p>
                                    )}
                                  </div>
                                  <Button
                                    variant="outline"
                                    size="icon"
                                    onClick={() => {
                                      if ((state.skillRefs || []).length < 20) {
                                        setState(prev => ({ ...prev, skillRefs: [...prev.skillRefs, ""] }));
                                      }
                                    }}
                                    title="Add skill"
                                  >
                                    <PlusCircle className="h-4 w-4" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    onClick={() => setState(prev => ({ ...prev, skillRefs: prev.skillRefs.filter((_, i) => i !== idx) }))}
                                    disabled={(state.skillRefs || []).length <= 1}
                                    title="Remove skill"
                                  >
                                    <Trash2 className="h-4 w-4 text-red-500" />
                                  </Button>
                                </div>
                              </div>
                            );
                          })}
                        </div>
                        {state.errors.skills && (
                          <p className="text-red-500 text-sm mt-2">❌ {state.errors.skills}</p>
                        )}
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </>
            )}
            <div className="flex justify-end">
              <Button className="bg-violet-500 hover:bg-violet-600" onClick={handleSaveAgent} disabled={state.isSubmitting || state.isLoading}>
                {state.isSubmitting ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    {isEditMode ? "Updating..." : "Creating..."}
                  </>
                ) : isEditMode ? (
                  "Update Agent"
                ) : (
                  "Create Agent"
                )}
              </Button>
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
  // Determine if in edit mode
  const searchParams = useSearchParams();
  const isEditMode = searchParams.get("edit") === "true";
  const agentName = searchParams.get("name");
  const agentNamespace = searchParams.get("namespace");

  // Create a key based on the edit mode and agent ID
  const formKey = isEditMode ? `edit-${agentName}-${agentNamespace}` : 'create';

  return (
    <Suspense fallback={<LoadingState />}>
      <AgentPageContent key={formKey} isEditMode={isEditMode} agentName={agentName} agentNamespace={agentNamespace} />
    </Suspense>
  );
}
