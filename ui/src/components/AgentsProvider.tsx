"use client";

import React, { createContext, useContext, useState, useEffect, ReactNode, useCallback } from "react";
import { getAgent as getAgentAction, createAgent, getAgents } from "@/app/actions/agents";
import { getTools } from "@/app/actions/tools";
import type { Agent, Tool, AgentResponse, RemoteMCPServerResponse, BaseResponse, ModelConfig, ToolsResponse, AgentType } from "@/types";
import { getModelConfigs } from "@/app/actions/modelConfigs";
import { isResourceNameValid } from "@/lib/utils";

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

export interface AgentFormData {
  name: string;
  namespace: string;
  description: string;
  type?: AgentType;
  // Declarative fields
  systemPrompt?: string;
  modelName?: string;
  tools: Tool[];
  stream?: boolean;
  byoImage?: string;
  byoCmd?: string;
  byoArgs?: string[];
  // Shared deployment optional fields
  replicas?: number;
  imagePullSecrets?: Array<{ name: string }>;
  volumes?: unknown[];
  volumeMounts?: unknown[];
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  env?: Array<{ name: string; value?: string }>;
  imagePullPolicy?: string;
  memory?: string[];
}

interface AgentsContextType {
  agents: AgentResponse[];
  models: ModelConfig[];
  loading: boolean;
  error: string;
  tools: ToolsResponse[];
  refreshAgents: () => Promise<void>;
  createNewAgent: (agentData: AgentFormData) => Promise<BaseResponse<Agent>>;
  updateAgent: (agentData: AgentFormData) => Promise<BaseResponse<Agent>>;
  getAgent: (name: string, namespace: string) => Promise<AgentResponse | null>;
  validateAgentData: (data: Partial<AgentFormData>) => ValidationErrors;
}

const AgentsContext = createContext<AgentsContextType | undefined>(undefined);

export function useAgents() {
  const context = useContext(AgentsContext);
  if (context === undefined) {
    throw new Error("useAgents must be used within an AgentsProvider");
  }
  return context;
}

interface AgentsProviderProps {
  children: ReactNode;
}

export function AgentsProvider({ children }: AgentsProviderProps) {
  const [agents, setAgents] = useState<AgentResponse[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [tools, setTools] = useState<ToolsResponse[]>([]);
  const [models, setModels] = useState<ModelConfig[]>([]);

  const fetchAgents = useCallback(async () => {
    try {
      setLoading(true);
      const agentsResult = await getAgents();

      if (!agentsResult.data || agentsResult.error) {
        throw new Error(agentsResult.error || "Failed to fetch agents");
      }

      setAgents(agentsResult.data);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchModels = useCallback(async () => {
    try {
      const response = await getModelConfigs();
      if (!response.data || response.error) {
        throw new Error(response.error || "Failed to fetch models");
      }

      setModels(response.data);
      setError("");
    } catch (err) {
      console.error("Error fetching models:", err);
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchTools = useCallback(async () => {
    try {
      setLoading(true);
      const response = await getTools();
      setTools(response);
      setError("");
    } catch (err) {
      console.error("Error fetching tools:", err);
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setLoading(false);
    }
  }, []);

  // Validation logic moved from the component
  const validateAgentData = useCallback((data: Partial<AgentFormData>): ValidationErrors => {
    const errors: ValidationErrors = {};

    if (data.name !== undefined) {
      if (!data.name.trim()) {
        errors.name = "Agent name is required";
      }
    }

    if (data.name !== undefined && !isResourceNameValid(data.name)) {
      errors.name = `Agent name can only contain lowercase alphanumeric characters, "-" or ".", and must start and end with an alphanumeric character`;
    }

    if (data.namespace !== undefined && data.namespace.trim()) {
      if (!isResourceNameValid(data.namespace)) {
        errors.namespace = `Agent namespace can only contain lowercase alphanumeric characters, "-" or ".", and must start and end with an alphanumeric character`;
      }
    }

    if (data.description !== undefined && !data.description.trim()) {
      errors.description = "Description is required";
    }

    const type = data.type || "Declarative";
    if (type === "Declarative") {
      if (data.systemPrompt !== undefined && !data.systemPrompt.trim()) {
        errors.systemPrompt = "Agent instructions are required";
      }
      if (!data.modelName || data.modelName.trim() === "") {
        errors.model = "Please select a model";
      }
    } else if (type === "BYO") {
      if (!data.byoImage || data.byoImage.trim() === "") {
        errors.model = "Container image is required";
      }
    }

    return errors;
  }, []);

  // Get agent by ID function
  const getAgent = useCallback(async (name: string, namespace: string): Promise<AgentResponse | null> => {
    try {
      // Fetch all agents
      const agentResult = await getAgentAction(name, namespace);
      if (!agentResult.data || agentResult.error) {
        console.error("Failed to get agent:", agentResult.error);
        setError("Failed to get agent");
        return null;
      }

      const agent = agentResult.data;
      
      if (!agent) {
        console.warn(`Agent with name ${name} and namespace ${namespace} not found`);
        return null;
      }
      return agent;
    } catch (error) {
      console.error("Error getting agent by name and namespace:", error);
      setError(error instanceof Error ? error.message : "Failed to get agent");
      return null;
    }
  }, []);

  // Agent creation logic moved from the component
  const createNewAgent = useCallback(async (agentData: AgentFormData) => {
    try {
      const errors = validateAgentData(agentData);
      if (Object.keys(errors).length > 0) {
        return { message: "Validation failed", error: "Validation failed", data: {} as Agent };
      }

      const result = await createAgent(agentData);

      if (!result.error) {
        // Refresh agents to get the newly created one
        await fetchAgents();
      }

      return result;
    } catch (error) {
      console.error("Error creating agent:", error);
      return {
        message: "Failed to create agent",
        error: error instanceof Error ? error.message : "Failed to create agent",
      };
    }
  }, [fetchAgents, validateAgentData]);

  // Update existing agent
  const updateAgent = useCallback(async (agentData: AgentFormData): Promise<BaseResponse<Agent>> => {
    try {
      const errors = validateAgentData(agentData);

      if (Object.keys(errors).length > 0) {
        console.log("Errors validating agent data", errors);
        return { message: "Validation failed", error: "Validation failed", data: {} as Agent };
      }

      // Use the same createAgent endpoint for updates
      const result = await createAgent(agentData, true);

      if (!result.error) {
        // Refresh agents to get the updated one
        await fetchAgents();
      }

      return result;
    } catch (error) {
      console.error("Error updating agent:", error);
      return {
        message: "Failed to update agent",
        error: error instanceof Error ? error.message : "Failed to update agent",
      };
    }
  }, [fetchAgents, validateAgentData]);

  // Initial fetches
  useEffect(() => {
    fetchAgents();
    fetchTools();
    fetchModels();
  }, [fetchAgents, fetchTools, fetchModels]);

  const value = {
    agents,
    models,
    loading,
    error,
    tools,
    refreshAgents: fetchAgents,
    createNewAgent,
    updateAgent,
    getAgent,
    validateAgentData,
  };

  return <AgentsContext.Provider value={value}>{children}</AgentsContext.Provider>;
}
