import { apiClient } from "../client";
import type { ToolServerResponse, ToolsResponse } from "../domain/mcpServers";
import { type ApiResource, useApiResource } from "./useApiResource";

/** The management catalog, or servers that permit references from one namespace. */
export function useMcpServers(referenceNamespace?: string): ApiResource<ToolServerResponse[]> {
  return useApiResource(referenceNamespace === "" ? null : ["mcpServers.list", referenceNamespace ?? ""], () =>
    apiClient.mcpServers.list({ referenceNamespace }),
  );
}

/** Every discovered tool, flattened across servers. */
export function useTools(): ApiResource<ToolsResponse[]> {
  return useApiResource(["tools.list"], () => apiClient.mcpServers.tools());
}
