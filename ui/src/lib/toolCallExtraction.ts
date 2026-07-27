import { Message, TextPart } from "@a2a-js/sdk";
import { ADKMetadata, ProcessedToolResultData, ToolResponseData, normalizeToolResultToText, getMetadataValue } from "@/lib/messageHandlers";
import { FunctionCall } from "@/types";
import { isAgentToolName } from "@/lib/utils";

// Helper functions to work with A2A SDK Messages carrying tool call data.
// Extracted from ToolCallDisplay so non-component code (e.g. ToolCallGroup
// summaries) can reuse them without pulling in component dependencies.

export const isToolCallRequestMessage = (message: Message): boolean => {
  // Check data parts for type metadata first
  const hasDataParts = message.parts?.some(part => {
    if (part.kind === "data" && part.metadata) {
      return getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_call";
    }
    return false;
  }) || false;

  // Fallback to streaming format check
  if (!hasDataParts) {
    const metadata = message.metadata as ADKMetadata;
    return metadata?.originalType === "ToolCallRequestEvent" || metadata?.originalType === "ToolApprovalRequest";
  }

  return hasDataParts;
};

export const isToolCallExecutionMessage = (message: Message): boolean => {
  const hasDataParts = message.parts?.some(part => {
    if (part.kind === "data" && part.metadata) {
      return getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_response";
    }
    return false;
  }) || false;

  // Fallback to streaming format check
  if (!hasDataParts) {
    const metadata = message.metadata as ADKMetadata;
    return metadata?.originalType === "ToolCallExecutionEvent";
  }

  return hasDataParts;
};

export const isToolCallSummaryMessage = (message: Message): boolean => {
  const metadata = message.metadata as ADKMetadata;
  return metadata?.originalType === "ToolCallSummaryMessage";
};

export const extractToolCallRequests = (message: Message): FunctionCall[] => {
  if (!isToolCallRequestMessage(message)) return [];

  // Check for stored task format first (data parts)
  const dataParts = message.parts?.filter(part => part.kind === "data") || [];
  const functionCalls: FunctionCall[] = [];

  for (const part of dataParts) {
    if (part.metadata) {
      if (getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_call") {
        const data = part.data as unknown as FunctionCall;
        // Skip ADK internal function calls (confirmation/auth) and ask_user (has its own display)
        if (
          data.name === "adk_request_confirmation" ||
          data.name === "adk_request_credential" ||
          data.name === "ask_user"
        ) {
          continue;
        }
        functionCalls.push({
          id: data.id,
          name: data.name,
          args: data.args ?? {},
        });
      }
    }
  }

  // If we found function calls in data parts, return them
  if (functionCalls.length > 0) {
    return functionCalls;
  }

  // Try streaming format (metadata or text content)
  const textParts = message.parts?.filter(part => part.kind === "text") || [];
  const content = textParts.map(part => (part as TextPart).text).join("");

  try {
    // Tool call data might be stored as JSON in content or metadata
    const metadata = message.metadata as ADKMetadata;
    const toolCallData = metadata?.toolCallData || JSON.parse(content || "[]");
    return Array.isArray(toolCallData)
      ? toolCallData.filter(tc =>
          tc.name !== "adk_request_confirmation" &&
          tc.name !== "adk_request_credential" &&
          tc.name !== "ask_user"
        )
      : [];
  } catch {
    return [];
  }
};

export const extractToolCallResults = (message: Message): ProcessedToolResultData[] => {
  if (!isToolCallExecutionMessage(message)) return [];

  // Check for stored task format first (data parts)
  const dataParts = message.parts?.filter(part => part.kind === "data") || [];
  const toolResults: ProcessedToolResultData[] = [];

  for (const part of dataParts) {
    if (part.metadata) {
      if (getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_response") {
        const data = part.data as unknown as ToolResponseData;

        // For agent tool responses we receive { result, subagent_session_id } as FunctionResponse.response.
        const textContent = normalizeToolResultToText(data);
        let subagentSessionId: string | undefined;
        if (isAgentToolName(data.name)) {
          const responseObj = data.response as Record<string, unknown> | undefined;
          if (responseObj && typeof responseObj.subagent_session_id === "string") {
            subagentSessionId = responseObj.subagent_session_id;
          }
        }

        toolResults.push({
          call_id: data.id,
          name: data.name,
          content: textContent,
          is_error: data.response?.isError || false,
          raw_result: data.response?.result ?? data.response,
          ...(subagentSessionId ? { subagent_session_id: subagentSessionId } : {}),
        });
      }
    }
  }

  // If we found tool results in data parts, return them
  if (toolResults.length > 0) {
    return toolResults;
  }

  // Try streaming format (metadata or text content)
  const textParts = message.parts?.filter(part => part.kind === "text") || [];
  const content = textParts.map(part => (part as TextPart).text).join("");

  try {
    const metadata = message.metadata as ADKMetadata;
    const resultData = metadata?.toolResultData || JSON.parse(content || "[]");
    return Array.isArray(resultData) ? resultData : [];
  } catch {
    return [];
  }
};
