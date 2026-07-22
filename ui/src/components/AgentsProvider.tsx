"use client";

import React, { createContext, useContext, useState, useEffect, ReactNode, useCallback } from "react";
import { getAgentWithResolvedKind, createAgent, getAgents } from "@/app/actions/agents";
import { getTools } from "@/app/actions/tools";
import type {
  Agent,
  AgentResponse,
  BaseResponse,
  ModelConfig,
  ToolsResponse,
} from "@/types";
import { getModelConfigs } from "@/app/actions/modelConfigs";
import type { AgentFormValidationErrors } from "@/components/agent-form/agent-form-types";
import {
  validateAgentFormData,
  type AgentFormData,
} from "@/lib/agentFormDomain";

export type { AgentFormData } from "@/lib/agentFormDomain";

export type ValidationErrors = AgentFormValidationErrors;

export interface AgentsContextType {
  agents: AgentResponse[];
  models: ModelConfig[];
  modelsLoaded: boolean;
  loading: boolean;
  error: string;
  tools: ToolsResponse[];
  toolsLoaded: boolean;
  refreshAgents: () => Promise<void>;
  refreshModels: () => Promise<void>;
  refreshTools: () => Promise<void>;
  createNewAgent: (agentData: AgentFormData) => Promise<BaseResponse<Agent>>;
  updateAgent: (agentData: AgentFormData) => Promise<BaseResponse<Agent>>;
  getAgent: (name: string, namespace: string) => Promise<AgentResponse | null>;
  validateAgentData: (data: Partial<AgentFormData>) => ValidationErrors;
}

export const AgentsContext = createContext<AgentsContextType | undefined>(undefined);

export function useAgents({
  loadModels = false,
  loadTools = false,
}: { loadModels?: boolean; loadTools?: boolean } = {}) {
  const context = useContext(AgentsContext);
  const refreshModels = context?.refreshModels;
  const refreshTools = context?.refreshTools;
  useEffect(() => {
    if (loadModels) void refreshModels?.();
  }, [loadModels, refreshModels]);
  useEffect(() => {
    if (loadTools) void refreshTools?.();
  }, [loadTools, refreshTools]);
  if (context === undefined) {
    throw new Error("useAgents must be used within an AgentsProvider");
  }
  return {
    ...context,
    loading:
      context.loading ||
      (loadModels && !context.modelsLoaded) ||
      (loadTools && !context.toolsLoaded),
  };
}

export interface AgentsProviderProps {
  children: ReactNode;
}

export function AgentsProvider({ children }: AgentsProviderProps) {
  const [agents, setAgents] = useState<AgentResponse[]>([]);
  const [agentError, setAgentError] = useState("");
  const [modelError, setModelError] = useState("");
  const [toolError, setToolError] = useState("");
  const [agentsLoading, setAgentsLoading] = useState(false);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [toolsLoading, setToolsLoading] = useState(false);
  const [tools, setTools] = useState<ToolsResponse[]>([]);
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [modelsLoaded, setModelsLoaded] = useState(false);
  const [toolsLoaded, setToolsLoaded] = useState(false);

  const fetchAgents = useCallback(async () => {
    try {
      setAgentsLoading(true);
      const agentsResult = await getAgents();

      if (!agentsResult.data || agentsResult.error) {
        throw new Error(agentsResult.error || "Failed to fetch agents");
      }

      setAgents(agentsResult.data);
      setAgentError("");
    } catch (err) {
      setAgentError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setAgentsLoading(false);
    }
  }, []);

  const fetchModels = useCallback(async () => {
    try {
      setModelsLoading(true);
      const response = await getModelConfigs();
      if (response.error) {
        throw new Error(response.error);
      }

      // An empty list is a valid result (e.g. no ModelConfigs deployed). The
      // backend omits `data` for empty collections (json omitempty), so treat
      // missing data as an empty list rather than a fetch failure.
      setModels(response.data ?? []);
      setModelError("");
    } catch (err) {
      console.error("Error fetching models:", err);
      setModelError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setModelsLoaded(true);
      setModelsLoading(false);
    }
  }, []);

  const fetchTools = useCallback(async () => {
    try {
      setToolsLoading(true);
      const response = await getTools();
      setTools(response);
      setToolError("");
    } catch (err) {
      console.error("Error fetching tools:", err);
      setToolError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setToolsLoaded(true);
      setToolsLoading(false);
    }
  }, []);

  const validateAgentData = validateAgentFormData;

  // Get agent by ID function
  const getAgent = useCallback(async (name: string, namespace: string): Promise<AgentResponse | null> => {
    try {
      // Fetch all agents
      const agentResult = await getAgentWithResolvedKind(name, namespace);
      if (!agentResult.data || agentResult.error) {
        console.error("Failed to get agent:", agentResult.error);
        setAgentError("Failed to get agent");
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
      setAgentError(error instanceof Error ? error.message : "Failed to get agent");
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

      return result;
    } catch (error) {
      console.error("Error creating agent:", error);
      return {
        message: "Failed to create agent",
        error: error instanceof Error ? error.message : "Failed to create agent",
      };
    }
  }, [validateAgentData]);

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

      return result;
    } catch (error) {
      console.error("Error updating agent:", error);
      return {
        message: "Failed to update agent",
        error: error instanceof Error ? error.message : "Failed to update agent",
      };
    }
  }, [validateAgentData]);

  const value = {
    agents,
    models,
    modelsLoaded,
    loading: agentsLoading || modelsLoading || toolsLoading,
    error: agentError || modelError || toolError,
    tools,
    toolsLoaded,
    refreshAgents: fetchAgents,
    refreshModels: fetchModels,
    refreshTools: fetchTools,
    createNewAgent,
    updateAgent,
    getAgent,
    validateAgentData,
  };

  return <AgentsContext.Provider value={value}>{children}</AgentsContext.Provider>;
}
