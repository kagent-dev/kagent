"use client";

import React, { createContext, useContext, useState, useEffect, ReactNode } from "react";
import { getTeams, createTeam } from "@/app/actions/teams";
import { Team, Component, ToolConfig } from "@/types/datamodel";
import { getBuiltInTools } from "@/app/actions/tools";
import { BaseResponse, Model } from "@/lib/types";
import { createTeamConfig, transformToAgentConfig } from "@/lib/agents";
import { isIdentifier } from "@/lib/utils";

interface ValidationErrors {
  name?: string;
  description?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
}

export interface AgentFormData {
  name: string;
  description: string;
  systemPrompt: string;
  model: Model;
  tools: Component<ToolConfig>[];
}

interface AgentsContextType {
  teams: Team[];
  loading: boolean;
  error: string;
  tools: Component<ToolConfig>[];
  refreshTeams: () => Promise<void>;
  createNewAgent: (agentData: AgentFormData) => Promise<BaseResponse<Team>>;
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
  const [teams, setTeams] = useState<Team[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [tools, setTools] = useState<Component<ToolConfig>[]>([]);

  const fetchTeams = async () => {
    try {
      setLoading(true);
      const teamsResult = await getTeams();

      if (!teamsResult.data || teamsResult.error) {
        throw new Error(teamsResult.error || "Failed to fetch teams");
      }

      setTeams(teamsResult.data);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setLoading(false);
    }
  };

  const fetchTools = async () => {
    try {
      setLoading(true);
      const response = await getBuiltInTools();
      if (response.success && response.data) {
        setTools(response.data);
        setError("");
      }
    } catch (err) {
      console.error("Error fetching tools:", error);
      setError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setLoading(false);
    }
  };

  // Validation logic moved from the component
  const validateAgentData = (data: Partial<AgentFormData>): ValidationErrors => {
    const errors: ValidationErrors = {};

    if (data.name !== undefined) {
      if (!data.name.trim()) {
        errors.name = "Agent name is required";
      } else if (!isIdentifier(data.name)) {
        errors.name = "Agent name can't contain spaces or special characters";
      } else if (data.name.length > 50) {
        errors.name = "Agent name must be less than 50 characters";
      }
    }

    if (data.description !== undefined && !data.description.trim()) {
      errors.description = "Description is required";
    }

    if (data.systemPrompt !== undefined && !data.systemPrompt.trim()) {
      errors.systemPrompt = "Agent instructions are required";
    }

    if (data.model === undefined) {
      errors.model = "Please select a model";
    }

    return errors;
  };

  // Agent creation logic moved from the component
  const createNewAgent = async (agentData: AgentFormData) => {
    try {
      const errors = validateAgentData(agentData);
      if (Object.keys(errors).length > 0) {
        return { success: false, error: "Validation failed", data: {} as Team };
      }

      const agentConfig = transformToAgentConfig(agentData);
      const teamConfig = await createTeamConfig(agentConfig);
      const result = await createTeam(teamConfig);

      if (result.success) {
        // Refresh teams to get the newly created one
        await fetchTeams();
      }

      return result;
    } catch (error) {
      console.error("Error creating agent:", error);
      return { 
        success: false, 
        error: error instanceof Error ? error.message : "Failed to create agent" 
      };
    }
  };

  // Initial fetches
  useEffect(() => {
    fetchTeams();
    fetchTools();
  }, []);

  const value = {
    teams,
    loading,
    error,
    tools,
    refreshTeams: fetchTeams,
    createNewAgent,
    validateAgentData
  };

  return <AgentsContext.Provider value={value}>{children}</AgentsContext.Provider>;
}