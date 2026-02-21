import React, { useEffect, useMemo, useRef } from "react";
import { Message, TextPart } from "@a2a-js/sdk";
import ToolDisplay, { ToolCallStatus } from "@/components/ToolDisplay";
import AgentCallDisplay from "@/components/chat/AgentCallDisplay";
import { isAgentToolName } from "@/lib/utils";
import { ADKMetadata, ProcessedToolResultData, ToolResponseData, normalizeToolResultToText, getMetadataValue } from "@/lib/messageHandlers";
import { FunctionCall } from "@/types";

interface ToolCallDisplayProps {
  currentMessage: Message;
  allMessages: Message[];
}

export interface ToolCallState {
  id: string;
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status: ToolCallStatus;
  author?: string;
}

const NAMESPACE_SEPARATOR = "__NS__";

/**
 * Extract the sub-agent name from an agent-as-tool call name.
 * e.g. "kagent__NS__k8s-agent" -> "k8s-agent"
 */
function extractAgentName(toolCallName: string): string {
  const lastIdx = toolCallName.lastIndexOf(NAMESPACE_SEPARATOR);
  if (lastIdx === -1) return toolCallName;
  return toolCallName.substring(lastIdx + NAMESPACE_SEPARATOR.length);
}

// Create a global cache to track tool calls across components
const toolCallCache = new Map<string, boolean>();

// Helper functions to work with A2A SDK Messages
const isToolCallRequestMessage = (message: Message): boolean => {
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
    return metadata?.originalType === "ToolCallRequestEvent";
  }

  return hasDataParts;
};

const isToolCallExecutionMessage = (message: Message): boolean => {
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

const isToolCallSummaryMessage = (message: Message): boolean => {
  const metadata = message.metadata as ADKMetadata;
  return metadata?.originalType === "ToolCallSummaryMessage";
};

const extractToolCallRequests = (message: Message): FunctionCall[] => {
  if (!isToolCallRequestMessage(message)) return [];

  // Check for stored task format first (data parts)
  const dataParts = message.parts?.filter(part => part.kind === "data") || [];
  const functionCalls: FunctionCall[] = [];

  for (const part of dataParts) {
    if (part.metadata) {
      if (getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_call") {
        const data = part.data as unknown as FunctionCall;
        functionCalls.push({
          id: data.id,
          name: data.name,
          args: data.args
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
    return Array.isArray(toolCallData) ? toolCallData : [];
  } catch {
    return [];
  }
};

const extractToolCallResults = (message: Message): ProcessedToolResultData[] => {
  if (!isToolCallExecutionMessage(message)) return [];

  // Check for stored task format first (data parts)
  const dataParts = message.parts?.filter(part => part.kind === "data") || [];
  const toolResults: ProcessedToolResultData[] = [];

  for (const part of dataParts) {
    if (part.metadata) {
      if (getMetadataValue<string>(part.metadata as Record<string, unknown>, "type") === "function_response") {
        const data = part.data as unknown as ToolResponseData;
        // Extract normalized content from the result (supports string/object/array)
        const textContent = normalizeToolResultToText(data);

        toolResults.push({
          call_id: data.id,
          name: data.name,
          content: textContent,
          is_error: data.response?.isError || false
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const content = textParts.map(part => (part as any).text).join("");

  try {
    const metadata = message.metadata as ADKMetadata;
    const resultData = metadata?.toolResultData || JSON.parse(content || "[]");
    return Array.isArray(resultData) ? resultData : [];
  } catch {
    return [];
  }
};

/**
 * Extract the author metadata from a message.
 * Checks both stored-task format (data part metadata) and streaming format (message metadata).
 */
const extractAuthor = (message: Message): string | undefined => {
  // Try message-level metadata first (streaming format preserves author here)
  const msgAuthor = getMetadataValue<string>(message.metadata as Record<string, unknown>, "author");
  if (msgAuthor) return msgAuthor;

  // For stored tasks, data parts may carry their own metadata
  for (const part of message.parts || []) {
    if (part.kind === "data" && part.metadata) {
      const partAuthor = getMetadataValue<string>(part.metadata as Record<string, unknown>, "author");
      if (partAuthor) return partAuthor;
    }
  }

  return undefined;
};

const ToolCallDisplay = ({ currentMessage, allMessages }: ToolCallDisplayProps) => {
  // Track which call IDs this component instance registered in the cache
  const registeredIdsRef = useRef<Set<string>>(new Set());

  // Compute owned call IDs based on current message (memoized)
  const ownedCallIds = useMemo(() => {
    const currentOwnedIds = new Set<string>();
    if (isToolCallRequestMessage(currentMessage)) {
      const requests = extractToolCallRequests(currentMessage);
      for (const request of requests) {
        if (request.id && !toolCallCache.has(request.id)) {
          currentOwnedIds.add(request.id);
          toolCallCache.set(request.id, true);
        }
      }
    }
    return currentOwnedIds;
  }, [currentMessage]);

  // Update ref and handle cleanup
  useEffect(() => {
    // Store current owned IDs for cleanup
    registeredIdsRef.current = ownedCallIds;

    return () => {
      registeredIdsRef.current.forEach(id => {
        toolCallCache.delete(id);
      });
    };
  }, [ownedCallIds]);

  // Compute tool calls based on all messages and owned IDs (memoized)
  const toolCalls = useMemo(() => {
    if (ownedCallIds.size === 0) {
      return new Map<string, ToolCallState>();
    }

    const newToolCalls = new Map<string, ToolCallState>();

    // First pass: collect all tool call requests that this component owns
    for (const message of allMessages) {
      if (isToolCallRequestMessage(message)) {
        const requests = extractToolCallRequests(message);
        const author = extractAuthor(message);
        for (const request of requests) {
          if (request.id && ownedCallIds.has(request.id)) {
            newToolCalls.set(request.id, {
              id: request.id,
              call: request,
              status: "requested",
              author,
            });
          }
        }
      }
    }

    // Second pass: update with execution results
    for (const message of allMessages) {
      if (isToolCallExecutionMessage(message)) {
        const results = extractToolCallResults(message);
        for (const result of results) {
          if (result.call_id && newToolCalls.has(result.call_id)) {
            const existingCall = newToolCalls.get(result.call_id)!;
            existingCall.result = {
              content: result.content,
              is_error: result.is_error
            };
            existingCall.status = "executing";
          }
        }
      }
    }

    // Third pass: mark completed calls using summary messages
    let summaryMessageEncountered = false;
    for (const message of allMessages) {
      if (isToolCallSummaryMessage(message)) {
        summaryMessageEncountered = true;
        break;
      }
    }

    if (summaryMessageEncountered) {
      newToolCalls.forEach((call, id) => {
        // Only update owned calls that are in 'executing' state and have a result
        if (call.status === "executing" && call.result && ownedCallIds.has(id)) {
          call.status = "completed";
        }
      });
    } else {
      // For stored tasks without summary messages, auto-complete tool calls that have results
      newToolCalls.forEach((call, id) => {
        if (call.status === "executing" && call.result && ownedCallIds.has(id)) {
          call.status = "completed";
        }
      });
    }

    return newToolCalls;
  }, [allMessages, ownedCallIds]);

  // Collect ALL tool calls across all messages (not just owned) for nesting lookup
  const allToolCalls = useMemo(() => {
    const all = new Map<string, ToolCallState>();

    for (const message of allMessages) {
      if (isToolCallRequestMessage(message)) {
        const requests = extractToolCallRequests(message);
        const author = extractAuthor(message);
        for (const request of requests) {
          if (request.id) {
            all.set(request.id, {
              id: request.id,
              call: request,
              status: "requested",
              author,
            });
          }
        }
      }
    }

    // Update with results
    for (const message of allMessages) {
      if (isToolCallExecutionMessage(message)) {
        const results = extractToolCallResults(message);
        for (const result of results) {
          if (result.call_id && all.has(result.call_id)) {
            const existingCall = all.get(result.call_id)!;
            existingCall.result = {
              content: result.content,
              is_error: result.is_error
            };
            existingCall.status = "completed";
          }
        }
      }
    }

    return all;
  }, [allMessages]);

  // Build parent-child map: agent call ID -> nested tool call states
  const nestedCallsMap = useMemo(() => {
    const map = new Map<string, ToolCallState[]>();
    const allCalls = Array.from(allToolCalls.values());

    // For each agent-as-tool call, find child calls whose author matches the sub-agent name
    for (const call of allCalls) {
      if (isAgentToolName(call.call.name)) {
        const subAgentName = extractAgentName(call.call.name);
        const children = allCalls.filter(c =>
          c.id !== call.id && c.author === subAgentName
        );
        if (children.length > 0) {
          map.set(call.id, children);
        }
      }
    }

    return map;
  }, [allToolCalls]);

  // Determine which call IDs are nested children (should not render at top level)
  const nestedChildIds = useMemo(() => {
    const ids = new Set<string>();
    nestedCallsMap.forEach(children => {
      for (const child of children) {
        ids.add(child.id);
      }
    });
    return ids;
  }, [nestedCallsMap]);

  // If no tool calls to display for this message, return null
  const currentDisplayableCalls = Array.from(toolCalls.values()).filter(
    call => ownedCallIds.has(call.id) && !nestedChildIds.has(call.id)
  );
  if (currentDisplayableCalls.length === 0) return null;

  return (
    <div className="space-y-2">
      {currentDisplayableCalls.map(toolCall => (
        isAgentToolName(toolCall.call.name) ? (
          <AgentCallDisplay
            key={toolCall.id}
            call={toolCall.call}
            result={toolCall.result}
            status={toolCall.status}
            isError={toolCall.result?.is_error}
            nestedCalls={nestedCallsMap.get(toolCall.id)}
          />
        ) : (
          <ToolDisplay
            key={toolCall.id}
            call={toolCall.call}
            result={toolCall.result}
            status={toolCall.status}
            isError={toolCall.result?.is_error}
          />
        )
      ))}
    </div>
  );
};

export default ToolCallDisplay;
export type { ToolCallDisplayProps };
