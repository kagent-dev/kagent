"use client";
import React, { useState, useEffect, Suspense } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Loader2, Settings2 } from "lucide-react";
import { ModelConfig, MemoryResponse } from "@/lib/types";
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
import { Tool } from "@/types/datamodel";
import { toast } from "sonner";
import { listMemories } from "@/app/actions/memories";

interface ValidationErrors {
  name?: string;
  description?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
  memory?: string;
}

interface AgentPageContentProps {
  isEditMode: boolean;
  agentId: string | null;
}

const DEFAULT_SYSTEM_PROMPT = `# Instructions
    - If user question is unclear, ask for clarification before running any tools
    - Always be helpful and friendly
    - If you don't know how to answer the question DO NOT make things up, tell the user "Sorry, I don't know how to answer that" and ask them to clarify the question further
    - Do not delete the original Deployment until the user explicitly confirms that the Rollout is ready to take over production traffic.

# Response format:
    - ALWAYS format your response as Markdown
    - Your response will include a summary of actions you took and an explanation of the result
    - If you created any artifacts such as files or resources, you will include those in your response as well`

// Inner component that uses useSearchParams, wrapped in Suspense
function AgentPageContent({ isEditMode, agentId }: AgentPageContentProps) {
  const router = useRouter();
  const { models, tools, loading, error, createNewAgent, updateAgent, getAgentById, validateAgentData } = useAgents();

  // Basic form state
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [systemPrompt, setSystemPrompt] = useState(isEditMode ? "" : DEFAULT_SYSTEM_PROMPT);

  // Default to the first model
  type SelectedModelType = Pick<ModelConfig, 'name' | 'model'>;
  const [selectedModel, setSelectedModel] = useState<SelectedModelType | null>(models && models.length > 0 ? { name: models[0].name, model: models[0].model } : null);

  // Tools state - now using AgentTool interface correctly
  const [selectedTools, setSelectedTools] = useState<Tool[]>([]);

  // Memory state
  const [availableMemories, setAvailableMemories] = useState<MemoryResponse[]>([]);
  const [selectedMemories, setSelectedMemories] = useState<string[]>([]);

  // Overall form state
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isLoading, setIsLoading] = useState(isEditMode);
  const [errors, setErrors] = useState<ValidationErrors>({});

  useEffect(() => {
    if (models && models.length > 0 && !selectedModel) {
      setSelectedModel(models[0]);
    }
  }, [models, selectedModel]);

  // Fetch existing agent data if in edit mode
  useEffect(() => {
    const fetchAgentData = async () => {
      if (isEditMode && agentId) {
        try {
          setIsLoading(true);
          const agentResponse = await getAgentById(agentId);

          if (!agentResponse) {
            toast.error("Agent not found");
            setIsLoading(false);
            return;
          }
          const agent = agentResponse.agent;
          if (agent) {
            try {
              // Populate form with existing agent data
              setName(agent.metadata.name || "");
              setDescription(agent.spec.description || "");
              setSystemPrompt(agent.spec.systemMessage || "");
              setSelectedTools(agent.spec.tools || []);
              setSelectedModel({
                model: agentResponse.model,
                name: agent.spec.modelConfig,
              });
              
              // Set selected memories if they exist
              if (agent.spec.memory && Array.isArray(agent.spec.memory)) {
                setSelectedMemories(agent.spec.memory);
              }
            } catch (extractError) {
              console.error("Error extracting assistant data:", extractError);
              toast.error("Failed to extract agent data from team structure");
            }
          } else {
            toast.error("Agent not found");
          }
        } catch (error) {
          console.error("Error fetching agent:", error);
          toast.error("Failed to load agent data");
        } finally {
          setIsLoading(false);
        }
      }
    };

    fetchAgentData();
  }, [isEditMode, agentId, getAgentById]);

  useEffect(() => {
    const fetchMemories = async () => {
      try {
        const memories = await listMemories();
        setAvailableMemories(memories);
      } catch (error) {
        console.error("Error fetching memories:", error);
        toast.error("Failed to load available memories.");
      }
    };
    fetchMemories();
  }, []);

  const validateForm = () => {
    const formData = {
      name,
      description,
      systemPrompt,
      model: selectedModel || undefined,
      tools: selectedTools,
    };

    const newErrors = validateAgentData(formData);
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // Add field-level validation functions
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const validateField = (fieldName: keyof ValidationErrors, value: any) => {
    const formData: Partial<AgentFormData> = {};

    // Set only the field being validated
    switch (fieldName) {
      case 'name': formData.name = value; break;
      case 'description': formData.description = value; break;
      case 'systemPrompt': formData.systemPrompt = value; break;
      case 'model': formData.model = value; break;
      case 'tools': formData.tools = value; break;
      case 'memory': formData.memory = value; break;
    }

    const fieldErrors = validateAgentData(formData);

    // Update only the specific field error
    setErrors(prev => ({
      ...prev,
      [fieldName]: fieldErrors[fieldName]
    }));
  };

  const handleSaveAgent = async () => {
    if (!validateForm()) {
      return;
    }

    try {
      setIsSubmitting(true);
      if (!selectedModel) {
        throw new Error("Model is required to create the agent.");
      }

      const agentData = {
        name,
        systemPrompt,
        description,
        model: selectedModel,
        tools: selectedTools,
        memory: selectedMemories,
      };

      let result;

      if (isEditMode && agentId) {
        // Update existing agent
        result = await updateAgent(agentId, agentData);
      } else {
        // Create new agent
        result = await createNewAgent(agentData);
      }

      if (!result.success) {
        throw new Error(result.error || `Failed to ${isEditMode ? "update" : "create"} agent`);
      }

      router.push(`/agents`);
    } catch (error) {
      console.error(`Error ${isEditMode ? "updating" : "creating"} agent:`, error);
      const errorMessage = error instanceof Error ? error.message : `Failed to ${isEditMode ? "update" : "create"} agent. Please try again.`;
      toast.error(errorMessage);
    } finally {
      setIsSubmitting(false);
    }
  };

  const renderPageContent = () => {
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
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    onBlur={() => validateField('name', name)}
                    className={`${errors.name ? "border-red-500" : ""}`}
                    placeholder="Enter agent name..."
                    disabled={isSubmitting || isLoading || isEditMode}
                  />
                  {errors.name && <p className="text-red-500 text-sm mt-1">{errors.name}</p>}
                </div>

                <div>
                  <label className="text-base mb-2 block font-bold">Description</label>
                  <p className="text-xs mb-2 block text-muted-foreground">
                    This is a description of the agent. It's for your reference only and it's not going to be used by the agent.
                  </p>
                  <Textarea
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    onBlur={() => validateField('description', description)}
                    className={`min-h-[100px] ${errors.description ? "border-red-500" : ""}`}
                    placeholder="Describe your agent. This is for your reference only and it's not going to be used by the agent."
                    disabled={isSubmitting || isLoading}
                  />
                  {errors.description && <p className="text-red-500 text-sm mt-1">{errors.description}</p>}
                </div>

                <SystemPromptSection 
                  value={systemPrompt} 
                  onChange={(e) => setSystemPrompt(e.target.value)} 
                  onBlur={() => validateField('systemPrompt', systemPrompt)}
                  error={errors.systemPrompt} 
                  disabled={isSubmitting || isLoading} 
                />

                <ModelSelectionSection 
                  allModels={models} 
                  selectedModel={selectedModel} 
                  setSelectedModel={(model) => {
                    setSelectedModel(model as Pick<ModelConfig, 'name' | 'model'>);
                    validateField('model', model);
                  }} 
                  error={errors.model} 
                  isSubmitting={isSubmitting || isLoading} 
                  onBlur={() => validateField('model', selectedModel)}
                />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Settings2 className="h-5 w-5" />
                  Memory
                </CardTitle>
                  <p className="text-xs mb-2 block text-muted-foreground">
                    The memories that the agent will use to answer the user's questions.
                  </p>
              </CardHeader>
              <CardContent>
                <MemorySelectionSection
                  availableMemories={availableMemories}
                  selectedMemories={selectedMemories}
                  onSelectionChange={setSelectedMemories}
                  disabled={isSubmitting || isLoading}
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
                  allTools={tools} 
                  selectedTools={selectedTools} 
                  setSelectedTools={setSelectedTools} 
                  isSubmitting={isSubmitting || isLoading} 
                  onBlur={() => validateField('tools', selectedTools)}
                />
              </CardContent>
            </Card>
            <div className="flex justify-end">
              <Button className="bg-violet-500 hover:bg-violet-600" onClick={handleSaveAgent} disabled={isSubmitting || isLoading}>
                {isSubmitting ? (
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
      {(loading || isSubmitting || isLoading) && <LoadingState />}
      {renderPageContent()}
    </>
  );
}

// Main component that wraps the content in a Suspense boundary
export default function AgentPage() {
  // Determine if in edit mode
  const searchParams = useSearchParams();
  const isEditMode = searchParams.get("edit") === "true";
  const agentId = searchParams.get("id");
  
  // Create a key based on the edit mode and agent ID
  const formKey = isEditMode ? `edit-${agentId}` : 'create';
  
  return (
    <Suspense fallback={<LoadingState />}>
      <AgentPageContent key={formKey} isEditMode={isEditMode} agentId={agentId} />
    </Suspense>
  );
}
