import React, { useState, useEffect } from "react";
import { messageUtils } from "@/lib/utils";
import { AgentMessageConfig, ToolCallExecutionEvent, FunctionCall } from "@/types/datamodel";
import ToolDisplay, { ToolCallStatus } from "@/components/ToolDisplay";

interface ToolCallDisplayProps {
  currentMessage: AgentMessageConfig;
  allMessages: AgentMessageConfig[];
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

const ToolCallDisplay = ({ currentMessage, allMessages }: ToolCallDisplayProps) => {
  // Track tool calls with their status
  const [toolCalls, setToolCalls] = useState<Map<string, ToolCallState>>(new Map());
  // Track which call IDs this component instance is responsible for
  const [ownedCallIds, setOwnedCallIds] = useState<Set<string>>(new Set());

  useEffect(() => {
    const currentOwnedIds = new Set<string>();
    if (messageUtils.isToolCallRequestEvent(currentMessage)) {
      const requests = currentMessage.content;
      if (Array.isArray(requests)) {
        for (const request of requests) {
          if (request.id && !toolCallCache.has(request.id)) {
            currentOwnedIds.add(request.id);
            toolCallCache.set(request.id, true);
          }
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
      if (messageUtils.isToolCallRequestEvent(message)) {
        const requests = message.content;
        if (Array.isArray(requests)) {
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
    }

    // Second pass: update with execution results
    for (const message of allMessages) {
      if (messageUtils.isToolCallExecutionEvent(message)) {
        const executionEvent = message as ToolCallExecutionEvent;
        const results = executionEvent.content;

        if (Array.isArray(results)) {
          for (const result of results) {
            if (result.call_id && newToolCalls.has(result.call_id)) { // ownedCallIds.has check is implicitly covered by newToolCalls.has
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
    }

    // Third pass: mark completed calls using summary messages
    let summaryMessageEncountered = false;
    for (const message of allMessages) {
      if (messageUtils.isToolCallSummaryMessage(message)) {
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
        <ToolDisplay
          key={toolCall.id}
          call={toolCall.call}
          result={toolCall.result}
          status={toolCall.status}
          isError={toolCall.result?.is_error}
        />
      ))}
    </div>
  );
};

export default ToolCallDisplay;
