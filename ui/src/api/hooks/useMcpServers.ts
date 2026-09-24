import { apiClient } from "../client";
import type { ToolServerResponse, ToolsResponse } from "../domain/mcpServers";
import { type ApiResource, useApiResource } from "./useApiResource";

/** Every MCP / tool server registered with the controller. */
export function useMcpServers(): ApiResource<ToolServerResponse[]> {
  return useApiResource(["mcpServers.list"], () => apiClient.mcpServers.list());
}

/** Every discovered tool, flattened across servers. */
export function useTools(): ApiResource<ToolsResponse[]> {
  return useApiResource(["tools.list"], () => apiClient.mcpServers.tools());
}
