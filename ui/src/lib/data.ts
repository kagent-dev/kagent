import { Component, MCPToolConfig, ToolConfig } from "@/types/datamodel";
import type { Model } from "./types";

// TODO: Could also come from the backend
export const AVAILABLE_MODELS: Model[] = [
  { id: "gpt-4o", name: "GPT-4o" },
  { id: "gpt-4o-mini", name: "GPT-4o-mini" },
];

export const isMcpTool = (content: unknown): content is MCPToolConfig => {
  if (!content || typeof content !== "object") return false;

  const { config } = content as { config: unknown };
  if (!config || typeof config !== "object") return false;

  const { server_params, tool } = config as { server_params: unknown; tool: unknown };
  if (!server_params || typeof server_params !== "object" || !tool || typeof tool !== "object") return false;

  return true;
};

// All MCP tools have the same label & description, so the actual tool name is stored in the config
export const getToolDisplayName = (tool: Component<ToolConfig>) => (isMcpTool(tool) ? (tool.config as MCPToolConfig).tool.name : tool.label);
export const getToolDescription = (tool: Component<ToolConfig>) => (isMcpTool(tool) ? (tool.config as MCPToolConfig).tool.description : tool.description);
export const getToolIdentifier = (tool: Component<ToolConfig>): string => (isMcpTool(tool) ? (tool.config as MCPToolConfig).tool.name : `${tool.provider}::${getToolDisplayName(tool)}`);
