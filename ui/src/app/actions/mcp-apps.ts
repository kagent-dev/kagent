"use server";

import type { CallToolResult, ReadResourceResult } from "@modelcontextprotocol/sdk/types.js";
import type { BaseResponse } from "@/types";
import { createErrorResponse, fetchApi } from "./utils";

export interface McpAppTool {
  name: string;
  description?: string;
  inputSchema?: unknown;
  uiResourceUri?: string;
  _meta?: Record<string, unknown>;
}

function serverPath(namespace: string, name: string): string {
  return `/mcp-apps/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;
}

// A namespace/name pair is ambiguous across the two tool-server CRDs
// (RemoteMCPServer and MCPServer). When known, the caller passes the selected
// server's groupKind so the backend resolves the exact CRD the user intended.
function withGroupKind(path: string, groupKind?: string): string {
  if (!groupKind) {
    return path;
  }
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}groupKind=${encodeURIComponent(groupKind)}`;
}

export async function listMcpAppTools(namespace: string, name: string, groupKind?: string): Promise<BaseResponse<McpAppTool[]>> {
  try {
    return await fetchApi<BaseResponse<McpAppTool[]>>(withGroupKind(`${serverPath(namespace, name)}/tools`, groupKind));
  } catch (err) {
    return createErrorResponse(err, "Failed to list MCP app tools");
  }
}

export async function callMcpAppTool(
  namespace: string,
  name: string,
  toolName: string,
  args?: Record<string, unknown>,
  groupKind?: string,
): Promise<BaseResponse<CallToolResult>> {
  try {
    return await fetchApi<BaseResponse<CallToolResult>>(
      withGroupKind(`${serverPath(namespace, name)}/tools/${encodeURIComponent(toolName)}/call`, groupKind),
      {
        method: "POST",
        body: JSON.stringify({ arguments: args ?? {} }),
      },
    );
  } catch (err) {
    return createErrorResponse(err, "Failed to call MCP app tool");
  }
}

export async function readMcpAppResource(
  namespace: string,
  name: string,
  uri: string,
  groupKind?: string,
): Promise<BaseResponse<ReadResourceResult>> {
  try {
    return await fetchApi<BaseResponse<ReadResourceResult>>(
      withGroupKind(`${serverPath(namespace, name)}/resources?uri=${encodeURIComponent(uri)}`, groupKind),
    );
  } catch (err) {
    return createErrorResponse(err, "Failed to read MCP app resource");
  }
}
