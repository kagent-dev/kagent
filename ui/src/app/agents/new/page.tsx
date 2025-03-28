"use client";
import React, { useState, useEffect, Suspense } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Loader2, FunctionSquare } from "lucide-react";
import { Model } from "@/lib/types";
import { SystemPromptSection } from "@/components/create/SystemPromptSection";
import { ModelSelectionSection } from "@/components/create/ModelSelectionSection";
import { ToolsSection } from "@/components/create/ToolsSection";
import { useRouter, useSearchParams } from "next/navigation";
import { useAgents } from "@/components/AgentsProvider";
import type { AgentTool } from "@/types/datamodel";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import KagentLogo from "@/components/kagent-logo";
import { extractSocietyOfMindAgentTools } from "@/lib/toolUtils";

interface ValidationErrors {
  name?: string;
  description?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
}

// Inner component that uses useSearchParams, wrapped in Suspense
function AgentPageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { models, tools, loading, error, createNewAgent, updateAgent, getAgentById, validateAgentData } = useAgents();

  // Determine if in edit mode
  const isEditMode = searchParams.get("edit") === "true";
  const agentId = searchParams.get("id");

  // Basic form state
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [systemPrompt, setSystemPrompt] = useState("");

  // Default to the first model
  const [selectedModel, setSelectedModel] = useState<Model | null>(models && models.length > 0 ? models[0] : null);

  // Tools state - now using AgentTool interface correctly
  const [selectedTools, setSelectedTools] = useState<AgentTool[]>([]);

  // Overall form state
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isLoading, setIsLoading] = useState(isEditMode);
  const [errors, setErrors] = useState<ValidationErrors>({});
  const [generalError, setGeneralError] = useState("");

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
            setGeneralError("Agent not found");
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
              setSelectedTools(extractSocietyOfMindAgentTools(agentResponse) || []);

              setSelectedModel({
                model: agentResponse.model,
                name: agent.spec.modelConfigRef,
              });
            } catch (extractError) {
              console.error("Error extracting assistant data:", extractError);
              setGeneralError("Failed to extract agent data from team structure");
            }
          } else {
            setGeneralError("Agent not found");
          }
        } catch (error) {
          console.error("Error fetching agent:", error);
          setGeneralError("Failed to load agent data");
        } finally {
          setIsLoading(false);
        }
      }
    };

    fetchAgentData();
  }, [isEditMode, agentId, getAgentById, models]);

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

  const handleSaveAgent = async () => {
    if (!validateForm()) {
      return;
    }

    try {
      setIsSubmitting(true);
      setGeneralError("");
      if (!selectedModel) {
        throw new Error("Model is required to create the agent.");
      }

      const agentData = {
        name,
        description,
        systemPrompt,
        model: selectedModel,
        tools: selectedTools,
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
      setGeneralError(`Failed to ${isEditMode ? "update" : "create"} agent. Please try again.`);
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

          {generalError && (
            <Alert variant="destructive" className="mb-6">
              <AlertDescription>{generalError}</AlertDescription>
            </Alert>
          )}

          <div className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <KagentLogo className="h-5 w-5" />
                  Basic Information
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="text-sm mb-2 block">Agent Name</label>
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className={`${errors.name ? "border-red-500" : ""}`}
                    placeholder="Enter agent name..."
                    disabled={isSubmitting || isLoading}
                  />
                  {errors.name && <p className="text-red-500 text-sm mt-1">{errors.name}</p>}
                </div>

                <div>
                  <label className="text-sm mb-2 block">Description</label>
                  <Textarea
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    className={`min-h-[100px] ${errors.description ? "border-red-500" : ""}`}
                    placeholder="Describe your agent. This is for your reference only and it's not going to be used by the agent."
                    disabled={isSubmitting || isLoading}
                  />
                  {errors.description && <p className="text-red-500 text-sm mt-1">{errors.description}</p>}
                </div>

                <SystemPromptSection value={systemPrompt} onChange={(e) => setSystemPrompt(e.target.value)} error={errors.systemPrompt} disabled={isSubmitting || isLoading} />

                <ModelSelectionSection allModels={models} selectedModel={selectedModel} setSelectedModel={setSelectedModel} error={errors.model} isSubmitting={isSubmitting || isLoading} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <FunctionSquare className="h-5 w-5 text-yellow-500" />
                  Tools
                </CardTitle>
              </CardHeader>
              <CardContent>
                <ToolsSection allTools={tools} selectedTools={selectedTools} setSelectedTools={setSelectedTools} isSubmitting={isSubmitting || isLoading} />
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
  return (
    <Suspense fallback={<LoadingState />}>
      <AgentPageContent />
    </Suspense>
  );
}
