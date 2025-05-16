import React, { useState, useEffect } from "react";
import { messageUtils } from "@/lib/utils";
import { AgentMessageConfig, ToolCallExecutionEvent, BaseMessageConfig, FunctionExecutionResult } from "@/types/datamodel";
import ToolDisplay from "@/components/ToolDisplay";

interface ToolCallDisplayProps {
  currentMessage: AgentMessageConfig;
  allMessages: AgentMessageConfig[];
}

const ToolCallDisplay = ({ currentMessage, allMessages }: ToolCallDisplayProps) => {
  // Track tool calls and their results
  const [toolState, setToolState] = useState(new Map());

  useEffect(() => {
    const newToolState = new Map();

    // Helper function to safely get the source from any message type
    const getMessageSource = (msg: AgentMessageConfig): string => {
      // Access source as a property of BaseMessageConfig which most types extend
      return (msg as BaseMessageConfig).source || "";
    };

    // Get all messages from the same source as the current message
    const currentSource = getMessageSource(currentMessage);
    const sourceMessages = allMessages.filter((msg) => getMessageSource(msg) === currentSource);

    // First pass: collect all tool calls from this source
    sourceMessages.forEach((message) => {
      if (messageUtils.isToolCallRequestEvent(message)) {
        // Only process tool calls from the current message
        if (message === currentMessage) {
          message.content.forEach((call) => {
            newToolState.set(call.id, {
              call,
              result: undefined,
            });
          });
        }
      }
    });

    // Second pass: match results with calls
    sourceMessages.forEach((message) => {
      // Check for ToolCallExecutionEvent which contains execution results
      if (messageUtils.isToolCallExecutionEvent(message)) {
        const execEvent = message as ToolCallExecutionEvent;
        // Handle the array of execution results inside execEvent.content
        execEvent.content.forEach((resultItem) => {
          if (resultItem.call_id) {
            // Create a proper FunctionExecutionResult from the execution data
            const functionResult: FunctionExecutionResult = {
              content: resultItem.content,
            };
            
            newToolState.set(resultItem.call_id, {
              ...newToolState.get(resultItem.call_id),
              result: functionResult,
            });
          }
        });
      }
      // Check for ToolCallSummaryMessage as an alternative
      else if (messageUtils.isToolCallSummaryMessage(message)) {
        // The summary message contains a string with the formatted result
        // We can't match it to specific calls, so we'll just use it for the first call
        // that doesn't have a result yet
        const summaryContent = message.content;
        if (typeof summaryContent === 'string') {
          // Find the first call without a result
          const callWithoutResult = Array.from(newToolState.entries()).find(
            ([entry]) => !entry.result
          );
          
          if (callWithoutResult) {
            const [callId, entry] = callWithoutResult;
            const functionResult: FunctionExecutionResult = {
              content: summaryContent,
            };
            
            newToolState.set(callId, {
              ...entry,
              result: functionResult,
            });
          }
        }
      }
    });

    setToolState(newToolState);
  }, [allMessages, currentMessage]);

  if (!toolState.size) return null;

  // Filter out any entries with undefined call properties to prevent errors
  const validEntries = Array.from(toolState.values()).filter(entry => entry && entry.call);

  return (
    <div className="space-y-2">
      {validEntries.map(({ call, result }) => (
        <ToolDisplay key={call.id} call={call} result={result} />
      ))}
    </div>
  );
};

export default ToolCallDisplay;
