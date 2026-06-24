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

export async function listMcpAppTools(namespace: string, name: string): Promise<BaseResponse<McpAppTool[]>> {
  try {
    return await fetchApi<BaseResponse<McpAppTool[]>>(`${serverPath(namespace, name)}/tools`);
  } catch (err) {
    return createErrorResponse(err, "Failed to list MCP app tools");
  }
}

export async function callMcpAppTool(
  namespace: string,
  name: string,
  toolName: string,
  args?: Record<string, unknown>,
): Promise<BaseResponse<CallToolResult>> {
  try {
    return await fetchApi<BaseResponse<CallToolResult>>(
      `${serverPath(namespace, name)}/tools/${encodeURIComponent(toolName)}/call`,
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
): Promise<BaseResponse<ReadResourceResult>> {
  try {
    return await fetchApi<BaseResponse<ReadResourceResult>>(
      `${serverPath(namespace, name)}/resources?uri=${encodeURIComponent(uri)}`,
    );
  } catch (err) {
    return createErrorResponse(err, "Failed to read MCP app resource");
  }
}
