import { Component, RunStatus, ToolConfig } from "@/types/datamodel";

export interface CreateAgentFormData {
  name: string;
  description: string;
  system_prompt: string;
  model: Model;
  tools: Component<ToolConfig>[];
}

export interface Model {
  name: string;
  model: string;
}

export interface CreateSessionRequest {
  userId: string;
  teamId: number;
}

export interface CreateRunRequest {
  user_id: string;
  session_id?: number;
}

export interface CreateRunResponse {
  run_id: string;
  status: RunStatus;
}

export interface BaseResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

export interface TokenStats {
  total: number;
  input: number;
  output: number;
}
