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
  loading: boolean;
  error: string;
  tools: ToolsResponse[];
  refreshAgents: () => Promise<void>;
  refreshModels: () => Promise<void>;
  refreshTools: () => Promise<void>;
  createNewAgent: (agentData: AgentFormData) => Promise<BaseResponse<Agent>>;
  updateAgent: (agentData: AgentFormData) => Promise<BaseResponse<Agent>>;
  getAgent: (name: string, namespace: string) => Promise<AgentResponse | null>;
  validateAgentData: (data: Partial<AgentFormData>) => ValidationErrors;
}

export const AgentsContext = createContext<AgentsContextType | undefined>(undefined);

export function useAgents() {
  const context = useContext(AgentsContext);
  if (context === undefined) {
    throw new Error("useAgents must be used within an AgentsProvider");
  }
  return context;
}

export interface AgentsProviderProps {
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
      if (response.error) {
        throw new Error(response.error);
      }

      // An empty list is a valid result (e.g. no ModelConfigs deployed). The
      // backend omits `data` for empty collections (json omitempty), so treat
      // missing data as an empty list rather than a fetch failure.
      setModels(response.data ?? []);
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

  const validateAgentData = validateAgentFormData;

  // Get agent by ID function
  const getAgent = useCallback(async (name: string, namespace: string): Promise<AgentResponse | null> => {
    try {
      // Fetch all agents
      const agentResult = await getAgentWithResolvedKind(name, namespace);
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

  // Initial fetches
  useEffect(() => {
    fetchTools();
    fetchModels();
  }, [fetchTools, fetchModels]);

  const value = {
    agents,
    models,
    loading,
    error,
    tools,
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
