import React, { useState, useEffect, useMemo } from "react";
import { Message, TextPart } from "@a2a-js/sdk";
import ToolDisplay, { ToolCallStatus } from "@/components/ToolDisplay";
import AgentCallDisplay from "@/components/chat/AgentCallDisplay";
import { isAgentToolName } from "@/lib/utils";
import { ADKMetadata, ProcessedToolResultData, ToolResponseData, normalizeToolResultToText } from "@/lib/messageHandlers";
import { FunctionCall } from "@/types";

interface ToolCallDisplayProps {
  currentMessage: Message;
  allMessages: Message[];
}

interface ToolCallState {
  id: string;
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
  };
  status: ToolCallStatus;
  nestedCalls?: ToolCallState[]; // Track nested agent calls
}

// Create a global cache to track tool calls across components
const toolCallCache = new Map<string, boolean>();

// Helper functions to work with A2A SDK Messages
const isToolCallRequestMessage = (message: Message): boolean => {
  // Check data parts for kagent_type first
  const hasDataParts = message.parts?.some(part => {
    if (part.kind === "data" && part.metadata) {
      const partMetadata = part.metadata as ADKMetadata;
      return partMetadata?.kagent_type === "function_call";
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
      const partMetadata = part.metadata as ADKMetadata;
      return partMetadata?.kagent_type === "function_response";
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
      const partMetadata = part.metadata as ADKMetadata;
      if (partMetadata?.kagent_type === "function_call") {
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
      const partMetadata = part.metadata as ADKMetadata;
      if (partMetadata?.kagent_type === "function_response") {
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

const ToolCallDisplay = ({ currentMessage, allMessages }: ToolCallDisplayProps) => {
  // Track tool calls with their status
  const [toolCalls, setToolCalls] = useState<Map<string, ToolCallState>>(new Map());
  // Track which call IDs this component instance is responsible for
  const [ownedCallIds, setOwnedCallIds] = useState<Set<string>>(new Set());

  useEffect(() => {
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
    setOwnedCallIds(currentOwnedIds);

    return () => {
      currentOwnedIds.forEach(id => {
        toolCallCache.delete(id);
      });
    };
  }, [currentMessage]);

  // Memoize the expensive nested call building operation
  const processedToolCalls = useMemo(() => {
    if (ownedCallIds.size === 0) {
      return new Map<string, ToolCallState>();
    }

    try {
      const newToolCalls = new Map<string, ToolCallState>();
      const allToolCallsMap = new Map<string, ToolCallState>(); // Track ALL tool calls for nesting

    // First pass: collect all tool call requests (both owned and nested)
    for (const message of allMessages) {
      if (isToolCallRequestMessage(message)) {
        const requests = extractToolCallRequests(message);
        for (const request of requests) {
          if (request.id) {
            const toolCallState: ToolCallState = {
              id: request.id,
              call: request,
              status: "requested",
              nestedCalls: []
            };

            allToolCallsMap.set(request.id, toolCallState);

            if (ownedCallIds.has(request.id)) {
              newToolCalls.set(request.id, toolCallState);
            }
          }
        }
      }
    }

    // Second pass: update with execution results
    for (const message of allMessages) {
      if (isToolCallExecutionMessage(message)) {
        const results = extractToolCallResults(message);
        for (const result of results) {
          if (result.call_id && allToolCallsMap.has(result.call_id)) {
            const existingCall = allToolCallsMap.get(result.call_id)!;
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
      allToolCallsMap.forEach((call) => {
        // Only update calls that are in 'executing' state and have a result
        if (call.status === "executing" && call.result) {
          call.status = "completed";
        }
      });
    } else {
      // For stored tasks without summary messages, auto-complete tool calls that have results
      allToolCallsMap.forEach((call) => {
        if (call.status === "executing" && call.result) {
          call.status = "completed";
        }
      });
    }

    // Fourth pass: Build nested call hierarchy
    // Map to track which message created which call (callId -> message that created it)
    const callIdToMessage = new Map<string, typeof allMessages[0]>();

    for (const message of allMessages) {
      if (isToolCallRequestMessage(message)) {
        const requests = extractToolCallRequests(message);
        for (const request of requests) {
          if (request.id) {
            callIdToMessage.set(request.id, message);
          }
        }
      }
    }

    // Build parent-child relationships
    // A call B is nested under call A if:
    // - Both A and B are agent calls
    // - B's message appeared after A's message but before A completed
    // - B is not in ownedCallIds (not a top-level call)
    allToolCallsMap.forEach((parentCall) => {
      if (!isAgentToolName(parentCall.call.name)) return;

      const nestedCallsList: ToolCallState[] = [];
      const parentMessage = callIdToMessage.get(parentCall.id);
      if (!parentMessage) return;

      const parentIndex = allMessages.indexOf(parentMessage);

      // Find where parent completed (if it did)
      let parentCompletionIndex = allMessages.length;
      for (let i = parentIndex + 1; i < allMessages.length; i++) {
        if (isToolCallExecutionMessage(allMessages[i])) {
          const results = extractToolCallResults(allMessages[i]);
          if (results.some(r => r.call_id === parentCall.id)) {
            parentCompletionIndex = i;
            break;
          }
        }
      }

      // Look for child calls between parent start and completion
      allToolCallsMap.forEach((potentialChild, childId) => {
        // Skip if it's the parent itself or if it's a top-level owned call
        if (childId === parentCall.id || ownedCallIds.has(childId)) return;

        const childMessage = callIdToMessage.get(childId);
        if (!childMessage) return;

        const childIndex = allMessages.indexOf(childMessage);

        // Child must appear after parent but before parent completes
        if (childIndex > parentIndex && childIndex < parentCompletionIndex) {
          nestedCallsList.push(potentialChild);
        }
      });

      parentCall.nestedCalls = nestedCallsList;
    });

      return newToolCalls;
    } catch (error) {
      console.error("Error building nested call hierarchy:", error);
      return new Map<string, ToolCallState>(); // Return empty map on error
    }
  }, [allMessages, ownedCallIds]);

  // Update state when processed calls change
  useEffect(() => {
    // Only update state if there's a change, to prevent unnecessary re-renders.
    let changed = processedToolCalls.size !== toolCalls.size;
    if (!changed) {
      for (const [key, value] of processedToolCalls) {
        const oldVal = toolCalls.get(key);
        if (!oldVal || oldVal.status !== value.status || oldVal.result?.content !== value.result?.content) {
          changed = true;
          break;
        }
      }
    }

    if (changed) {
        setToolCalls(processedToolCalls);
    }
  }, [processedToolCalls, toolCalls]);

  // If no tool calls to display for this message, return null
  const currentDisplayableCalls = Array.from(toolCalls.values()).filter(call => ownedCallIds.has(call.id));
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
            nestedCalls={toolCall.nestedCalls}
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
