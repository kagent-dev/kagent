import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

function isCallToolResult(value: unknown): value is CallToolResult {
  return typeof value === "object" && value !== null && Array.isArray((value as { content?: unknown }).content);
}

function stringifyToolPayload(value: unknown, fallback: string): string {
  if (typeof value === "string") {
    return value;
  }
  if (value === undefined || value === null) {
    return fallback;
  }
  try {
    return JSON.stringify(value);
  } catch {
    return fallback;
  }
}

/** Normalize tool-call args from chat history into a plain object for MCP Apps toolInput. */
export function normalizeMcpToolArgs(args: unknown): Record<string, unknown> {
  let value = args;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value) as unknown;
    } catch {
      return {};
    }
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return {};
  }
  const record = value as Record<string, unknown>;
  const nested = record.arguments;
  if (typeof nested === "object" && nested !== null && !Array.isArray(nested)) {
    return nested as Record<string, unknown>;
  }
  return record;
}

function unwrapAgentToolPayload(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }
  const record = value as Record<string, unknown>;
  if ("result" in record && typeof record.result === "object" && record.result !== null) {
    return unwrapAgentToolPayload(record.result) ?? (record.result as Record<string, unknown>);
  }
  return record;
}

function extractStructuredPayload(record: Record<string, unknown>): Record<string, unknown> | undefined {
  if (record.structuredContent != null && typeof record.structuredContent === "object" && !Array.isArray(record.structuredContent)) {
    return record.structuredContent as Record<string, unknown>;
  }
  if (record.output != null && typeof record.output === "object" && !Array.isArray(record.output)) {
    return record.output as Record<string, unknown>;
  }
  return undefined;
}

/**
 * Convert agent/MCP tool responses into a CallToolResult for @mcp-ui/client.
 * Handles ADK mcptoolset `{ output: structuredContent }` and full MCP results.
 */
export function toMcpAppToolResult(value: unknown, fallbackContent: string): CallToolResult | undefined {
  const record = unwrapAgentToolPayload(value);
  if (!record) {
    return undefined;
  }

  const structuredContent = extractStructuredPayload(record);
  const isError = record.isError === true || record.error === true;

  if (structuredContent) {
    return {
      content: isCallToolResult(record)
        ? record.content
        : [{ type: "text", text: stringifyToolPayload(structuredContent, fallbackContent) }],
      structuredContent,
      isError,
    };
  }

  if (isCallToolResult(record)) {
    return record;
  }

  return undefined;
}

/** Merge tool-call args (e.g. location) into structuredContent for MCP App widgets. */
export function withMcpAppToolInput(
  toolResult: CallToolResult | undefined,
  toolArgs: Record<string, unknown>,
): CallToolResult | undefined {
  if (!toolResult || Object.keys(toolArgs).length === 0) {
    return toolResult;
  }
  return {
    ...toolResult,
    structuredContent: {
      ...toolArgs,
      ...(toolResult.structuredContent ?? {}),
    },
  };
}

export function buildMcpAppRenderPayload(
  rawResult: unknown,
  fallbackContent: string,
  toolArgs: unknown,
): { toolInput: Record<string, unknown>; toolResult: CallToolResult | undefined } {
  const toolInput = normalizeMcpToolArgs(toolArgs);
  const toolResult = withMcpAppToolInput(
    toMcpAppToolResult(rawResult, fallbackContent),
    toolInput,
  );
  return { toolInput, toolResult };
}
