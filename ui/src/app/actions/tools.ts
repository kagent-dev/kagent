"use server";

import { BaseResponse } from "@/lib/types";
import { fetchApi, createErrorResponse } from "./utils";
import { Component, ToolConfig, MCPToolConfig } from "@/types/datamodel";
import { isMcpProvider } from "@/lib/toolUtils";

interface ToolResponse {
  name: string;
  component: Component<ToolConfig>;
  server_name: string;
}

/**
 * Gets all available tools
 * @returns A promise with all tools
 */
export async function getTools(): Promise<Component<ToolConfig>[]> {
  try {
    const response = await fetchApi<BaseResponse<ToolResponse[]>>("/tools");
    if (!response) {
      throw new Error("Failed to get built-in tools");
    }

    const toolsComponents = response.data?.map((t) => {
      // set the label in component to the server_name, because we use the server name (kagent-tool-server) to determine
      // whether a tool is a built-in tool or not.
      // TODO (peterj): Ideally, instead of returning the Component<ToolConfig> we could just directly return the actual ToolResponse.
      t.component.label = t.server_name;
      return t.component;
    });
    if (!toolsComponents) {
      throw new Error("Failed to get built-in tools");
    }

    // Convert API components to Component<ToolConfig> format
    const convertedTools = toolsComponents.map((tool) => {
      // Convert to Component<ToolConfig> format
      return {
        provider: tool.provider,
        label: tool.label || "",
        description: tool.description || "",
        config: tool.config || {},
        component_type: tool.component_type || "tool",
      } as Component<ToolConfig>;
    });

    return convertedTools || [];
  } catch (error) {
    throw new Error("Error getting built-in tools");
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
