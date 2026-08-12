"use server";

import type { CallToolResult, ReadResourceResult } from "@modelcontextprotocol/sdk/types.js";
import type { BaseResponse } from "@/types";
import { createErrorResponse } from "./utils";
import { getToolGrpcGateway } from "@/lib/grpc/client";

export interface McpAppTool {
  name: string;
  description?: string;
  inputSchema?: unknown;
  uiResourceUri?: string;
  _meta?: Record<string, unknown>;
}

export async function listMcpAppTools(namespace: string, name: string, groupKind?: string): Promise<BaseResponse<McpAppTool[]>> {
  try {
    const gateway = await getToolGrpcGateway();
    return {
      message: "Successfully listed MCP app tools",
      data: await gateway.listMcpAppTools(namespace, name, groupKind),
    };
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
    const gateway = await getToolGrpcGateway();
    return {
      message: "Successfully called MCP app tool",
      data: await gateway.callMcpAppTool(namespace, name, toolName, args, groupKind),
    };
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
    const gateway = await getToolGrpcGateway();
    return {
      message: "Successfully read MCP app resource",
      data: await gateway.readMcpAppResource(namespace, name, uri, groupKind),
    };
  } catch (err) {
    return createErrorResponse(err, "Failed to read MCP app resource");
  }
}
