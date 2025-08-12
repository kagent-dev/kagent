"use client";
import React, { useState, useEffect, Suspense } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Loader2, Settings2 } from "lucide-react";
import { ModelConfig, MemoryResponse, AgentType } from "@/types";
import { SystemPromptSection } from "@/components/create/SystemPromptSection";
import { ModelSelectionSection } from "@/components/create/ModelSelectionSection";
import { ToolsSection } from "@/components/create/ToolsSection";
import { MemorySelectionSection } from "@/components/create/MemorySelectionSection";
import { useRouter, useSearchParams } from "next/navigation";
import { useAgents } from "@/components/AgentsProvider";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import KagentLogo from "@/components/kagent-logo";
import { AgentFormData } from "@/components/AgentsProvider";
import { Tool } from "@/types";
import { toast } from "sonner";
import { listMemories } from "@/app/actions/memories";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Label } from "@/components/ui/label";
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
  memory?: string;
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
    availableMemories: MemoryResponse[];
    selectedMemories: string[];
    byoImage: string;
    byoCmd: string;
    byoArgs: string;
    isSubmitting: boolean;
    isLoading: boolean;
    errors: ValidationErrors;
  }

  const [state, setState] = useState<FormState>({
    name: "",
    namespace: "default",
    description: "",
    agentType: "Inline",
    systemPrompt: isEditMode ? "" : DEFAULT_SYSTEM_PROMPT,
    selectedModel: null,
    selectedTools: [],
    availableMemories: [],
    selectedMemories: [],
    byoImage: "",
    byoCmd: "",
    byoArgs: "",
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
              const agentTypeValue = (agent.spec?.type as "Inline" | "BYO") || "Inline";
              const baseUpdates: Partial<FormState> = {
                name: agent.metadata.name || "",
                namespace: agent.metadata.namespace || "",
                description: agent.spec?.description || "",
                agentType: agentTypeValue,
              };
              // v1alpha2: read type and split specs
              if (agentTypeValue === "Inline") {
                setState(prev => ({
                  ...prev,
                  ...baseUpdates,
                  systemPrompt: agent.spec?.inline?.systemMessage || "",
                  selectedTools: (agent.spec?.inline?.tools && agentResponse.tools) ? agentResponse.tools : [],
                  selectedModel: agentResponse.modelConfigRef ? { model: agentResponse.model || "default-model-config", ref: agentResponse.modelConfigRef } : null,
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
                }));
              }

              // Set selected memories if they exist
              if (agentResponse.memoryRefs && Array.isArray(agentResponse.memoryRefs)) {
                setState(prev => ({ ...prev, selectedMemories: agentResponse.memoryRefs }));
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

    fetchAgentData();
  }, [isEditMode, agentName, agentNamespace, getAgent]);

  useEffect(() => {
    const fetchMemories = async () => {
      try {
        const memories = await listMemories();
        setState(prev => ({ ...prev, availableMemories: memories }));
      } catch (error) {
        console.error("Error fetching memories:", error);
        toast.error("Failed to load available memories.");
      }
    };
    fetchMemories();
  }, []);

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
      case 'memory': formData.memory = value; break;
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
      if (state.agentType === "Inline" && !state.selectedModel) {
        throw new Error("Model is required to create Inline agent.");
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
        memory: state.selectedMemories,
        // BYO
        byoImage: state.byoImage,
        byoCmd: state.byoCmd || undefined,
        byoArgs: state.byoArgs ? state.byoArgs.split(/\s+/).filter(Boolean) : undefined,
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
                    Choose Inline (uses a model) or BYO (bring your own containerized agent).
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
                      <SelectItem value="Inline">Inline</SelectItem>
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

                {state.agentType === "Inline" && (
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
                  </div>
                )}
              </CardContent>
            </Card>
            {state.agentType === "Inline" && (
              <>
                <Card>
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <Settings2 className="h-5 w-5" />
                      Memory
                    </CardTitle>
                      <p className="text-xs mb-2 block text-muted-foreground">
                        The memories that the agent will use to answer the user&apos;s questions.
                      </p>
                  </CardHeader>
                  <CardContent>
                    <MemorySelectionSection
                      availableMemories={state.availableMemories}
                      selectedMemories={state.selectedMemories}
                      onSelectionChange={(mems) => setState(prev => ({ ...prev, selectedMemories: mems }))}
                      disabled={state.isSubmitting || state.isLoading}
                    />
                  </CardContent>
                </Card>
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
