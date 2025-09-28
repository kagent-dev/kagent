import React, { useState, useEffect } from "react";
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
  for (const part of dataParts) {
    if (part.metadata) {
      const partMetadata = part.metadata as ADKMetadata;
      if (partMetadata?.kagent_type === "function_call") {
        const data = part.data as unknown as FunctionCall;
        return [{
          id: data.id,
          name: data.name,
          args: data.args
        }];
      }
    }
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
  for (const part of dataParts) {
    if (part.metadata) {
      const partMetadata = part.metadata as ADKMetadata;
      if (partMetadata?.kagent_type === "function_response") {
        const data = part.data as unknown as ToolResponseData;
        // Extract normalized content from the result (supports string/object/array)
        const textContent = normalizeToolResultToText(data);
        
        return [{
          call_id: data.id,
          name: data.name,
          content: textContent,
          is_error: data.response?.isError || false
        }];
      }
    }
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

  useEffect(() => {
    if (ownedCallIds.size === 0) {
      // If the component doesn't own any call IDs, ensure toolCalls is empty and return.
      if (toolCalls.size > 0) {
        setToolCalls(new Map());
      }
      return;
    }

    const newToolCalls = new Map<string, ToolCallState>();

    // First pass: collect all tool call requests that this component owns
    for (const message of allMessages) {
      if (isToolCallRequestMessage(message)) {
        const requests = extractToolCallRequests(message);
        for (const request of requests) {
          if (request.id && ownedCallIds.has(request.id)) {
            newToolCalls.set(request.id, {
              id: request.id,
              call: request,
              status: "requested"
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
    
    // Only update state if there's a change, to prevent unnecessary re-renders.
    // This is a shallow comparison, but sufficient for this case.
    let changed = newToolCalls.size !== toolCalls.size;
    if (!changed) {
      for (const [key, value] of newToolCalls) {
        const oldVal = toolCalls.get(key);
        if (!oldVal || oldVal.status !== value.status || oldVal.result?.content !== value.result?.content) {
          changed = true;
          break;
        }
      }
    }

    if (changed) {
        setToolCalls(newToolCalls);
    }

  }, [allMessages, ownedCallIds, toolCalls]);

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
