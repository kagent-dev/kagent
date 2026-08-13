import type { CallToolResult, ReadResourceResult } from "@modelcontextprotocol/sdk/types.js";
import { fn } from "storybook/test";

import type { BaseResponse } from "@/types";

export interface McpAppTool {
  name: string;
  description?: string;
  inputSchema?: unknown;
  uiResourceUri?: string;
  _meta?: Record<string, unknown>;
}

export const listMcpAppTools = fn<
  (namespace: string, name: string, groupKind?: string) => Promise<BaseResponse<McpAppTool[]>>
>();
export const callMcpAppTool = fn<
  (
    namespace: string,
    name: string,
    toolName: string,
    args?: Record<string, unknown>,
    groupKind?: string,
  ) => Promise<BaseResponse<CallToolResult>>
>();
export const readMcpAppResource = fn<
  (
    namespace: string,
    name: string,
    uri: string,
    groupKind?: string,
  ) => Promise<BaseResponse<ReadResourceResult>>
>();
