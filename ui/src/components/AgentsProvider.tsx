"use client";

import React, { createContext, useContext, ReactNode, useCallback } from "react";
import { getAgent as getAgentAction, createAgent } from "@/app/actions/agents";
import type { Agent, Tool, AgentResponse, BaseResponse, ModelConfig, ToolsResponse, AgentType, EnvVar } from "@/types";
import { isResourceNameValid } from "@/lib/utils";
import { useSettings } from "@/contexts/SettingsContext";
import useSWR from "swr";
import { createTypedFetcher, buildApiUrl } from "@/lib/fetcher";

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
  env?: EnvVar[];
  imagePullPolicy?: string;
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
  isRefreshing: boolean;
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
  const { getSwrConfig } = useSettings();
  
  // Create typed fetchers
  const agentsFetcher = createTypedFetcher<AgentResponse[]>();
  const toolsFetcher = createTypedFetcher<ToolsResponse[]>();
  const modelsFetcher = createTypedFetcher<ModelConfig[]>();
  
  // SWR hooks for data fetching with auto-refresh
  const { data: agents = [], error: agentsError, isLoading: agentsLoading, mutate: mutateAgents } = useSWR(
    buildApiUrl('/agents'),
    agentsFetcher,
    getSwrConfig()
  );
  
  const { data: tools = [], error: toolsError, isLoading: toolsLoading } = useSWR(
    buildApiUrl('/tools'),
    toolsFetcher,
    // Tools change less frequently, so less aggressive refresh
    { revalidateOnFocus: false, refreshInterval: undefined }
  );
  
  const { data: models = [], error: modelsError, isLoading: modelsLoading } = useSWR(
    buildApiUrl('/models'),
    modelsFetcher,
    // Models change even less frequently
    { revalidateOnFocus: false, refreshInterval: undefined }
  );
  
  // Combine loading states
  const loading = agentsLoading || toolsLoading || modelsLoading;
  const error = agentsError?.message || toolsError?.message || modelsError?.message || "";
  const isRefreshing = agentsLoading && agents.length > 0; // Only consider it "refreshing" if we have data already

  // Manual refresh functions using SWR's mutate
  const refreshAgents = useCallback(async () => {
    await mutateAgents();
  }, [mutateAgents]);
  

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
      // Fetch single agent
      const agentResult = await getAgentAction(name, namespace);
      if (!agentResult.data || agentResult.error) {
        console.error("Failed to get agent:", agentResult.error);
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
      return null;
    }
  }, []);

  // Agent creation logic
  const createNewAgent = useCallback(async (agentData: AgentFormData) => {
    try {
      const errors = validateAgentData(agentData);
      if (Object.keys(errors).length > 0) {
        return { message: "Validation failed", error: "Validation failed", data: {} as Agent };
      }

      const result = await createAgent(agentData);

      if (!result.error) {
        // Refresh agents to get the newly created one
        await mutateAgents();
      }

      return result;
    } catch (error) {
      console.error("Error creating agent:", error);
      return {
        message: "Failed to create agent",
        error: error instanceof Error ? error.message : "Failed to create agent",
      };
    }
  }, [mutateAgents, validateAgentData]);

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
        await mutateAgents();
      }

      return result;
    } catch (error) {
      console.error("Error updating agent:", error);
      return {
        message: "Failed to update agent",
        error: error instanceof Error ? error.message : "Failed to update agent",
      };
    }
  }, [mutateAgents, validateAgentData]);

  const value = {
    agents,
    models,
    loading,
    error,
    tools,
    refreshAgents,
    createNewAgent,
    updateAgent,
    getAgent,
    validateAgentData,
    isRefreshing,
  };

  return <AgentsContext.Provider value={value}>{children}</AgentsContext.Provider>;
}
