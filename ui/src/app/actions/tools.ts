"use server";

import { BaseResponse } from "@/lib/types";
import { fetchApi, createErrorResponse } from "./utils";
import { Component, ToolConfig, MCPToolConfig } from "@/types/datamodel";
import { isMcpProvider } from "@/lib/toolUtils";

/**
 * Gets all available tools
 * @returns A promise with all tools
 */
export async function getTools(): Promise<BaseResponse<Component<ToolConfig>[]>> {
  try {
    const response = await fetchApi<Component<ToolConfig>[]>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }

    // Convert API components to Component<ToolConfig> format
    const convertedTools = response.map((tool) => {
      // Convert to Component<ToolConfig> format
      return {
        provider: tool.provider,
        label: tool.label || "",
        description: tool.description || "",
        config: tool.config || {},
        component_type: tool.component_type || "tool",
      } as Component<ToolConfig>;
    });

    return { success: true, data: convertedTools };
  } catch (error) {
    return createErrorResponse<Component<ToolConfig>[]>(error, "Error getting built-in tools");
  }
}

/**
 * Gets a specific tool by its provider name and optionally tool name
 * @param allTools The list of all tools
 * @param provider The tool provider name
 * @param toolName Optional tool name for MCP tools
 * @returns A promise with the tool data
 */
export async function getToolByProvider(allTools: Component<ToolConfig>[], provider: string, toolName?: string): Promise<Component<ToolConfig> | null> {
  // For MCP tools, we need to match both provider and tool name
  if (isMcpProvider(provider) && toolName) {
    const tool = allTools.find(t =>
      t.provider === provider &&
      (t.config as MCPToolConfig)?.tool?.name === toolName
    );

    if (tool) {
      // For MCP tools, use the description from the tool object
      return {
        ...tool,
        description: (tool.config as MCPToolConfig)?.tool?.description || "No description available"
      }
    };
  } else {
    // For non-MCP tools, just match the provider
    const tool = allTools.find(t => t.provider === provider);
    if (tool) {
      return tool;
    }
  }

  return null;
}
