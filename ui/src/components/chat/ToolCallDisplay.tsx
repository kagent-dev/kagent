import React, { useMemo } from "react";
import { Message } from "@a2a-js/sdk";
import ToolDisplay, { ToolCallStatus } from "@/components/ToolDisplay";
import AgentCallDisplay, { AgentCallStatus } from "@/components/chat/AgentCallDisplay";
import { isAgentToolName } from "@/lib/utils";
import { ADKMetadata, ProcessedToolCallData, getMetadataValue } from "@/lib/messageHandlers";
import {
  isToolCallRequestMessage,
  isToolCallExecutionMessage,
  isToolCallSummaryMessage,
  extractToolCallRequests,
  extractToolCallResults,
} from "@/lib/toolCallExtraction";
import { FunctionCall, ToolDecision, TokenStats } from "@/types";
import type { ChatMcpAppTool } from "@/components/chat/ChatMcpAppsContext";

interface ToolCallDisplayProps {
  currentMessage: Message;
  allMessages: Message[];
  onApprove?: (toolCallId: string) => void;
  onReject?: (toolCallId: string, reason?: string) => void;
  pendingDecisions?: Record<string, ToolDecision>;
  getMcpAppForTool?: (toolName: string) => ChatMcpAppTool | undefined;
  onMcpAppSendMessage?: (text: string) => Promise<void>;
}

interface ToolCallState {
  id: string;
  call: FunctionCall;
  result?: {
    content: string;
    is_error?: boolean;
    rawResult?: unknown;
  };
  status: ToolCallStatus;
  subagentSessionId?: string;
}

const ToolCallDisplay = ({ currentMessage, allMessages, onApprove, onReject, pendingDecisions, getMcpAppForTool, onMcpAppSendMessage }: ToolCallDisplayProps) => {
  // Determine which tool call IDs this component instance "owns" by finding,
  // for each ID introduced by currentMessage, whether currentMessage is the
  // FIRST message in allMessages that introduces that ID.
  const ownedCallIds = useMemo(() => {
    if (!isToolCallRequestMessage(currentMessage)) {
      return new Set<string>();
    }

    const currentRequests = extractToolCallRequests(currentMessage);
    if (currentRequests.length === 0) {
      return new Set<string>();
    }

    // Find the index of currentMessage in allMessages
    const currentIndex = allMessages.indexOf(currentMessage);
    if (currentIndex <= 0) {
      // If it's the first message (or not found), it owns all its requests
      return new Set(currentRequests.map(r => r.id).filter(id => id !== undefined) as string[]);
    }

    const ownedIds = new Set(currentRequests.map(r => r.id).filter(id => id !== undefined) as string[]);

    // Scan backwards from our index to see if any earlier message already has these IDs.
    // This avoids a full O(N) scan per component render by aborting early.
    for (let i = currentIndex - 1; i >= 0; i--) {
      const msg = allMessages[i];
      if (!isToolCallRequestMessage(msg)) continue;

      const prevRequests = extractToolCallRequests(msg);
      for (const pr of prevRequests) {
        if (pr.id) {
          ownedIds.delete(pr.id);
        }
      }

      if (ownedIds.size === 0) break; // Early exit if all IDs were claimed by earlier messages
    }
    return ownedIds;
  }, [currentMessage, allMessages]);

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
        for (const request of requests) {
          if (request.id && ownedCallIds.has(request.id)) {
            // For approval requests, set status based on whether a decision
            // was already made (resolved on reload) or is still pending.
            const msgMetadata = message.metadata as ADKMetadata;
            let initialStatus: ToolCallStatus = "requested";
            if (msgMetadata?.originalType === "ToolApprovalRequest") {
              const rawDecision = msgMetadata?.approvalDecision;
              // approvalDecision is either a uniform ToolDecision string
              // or a per-tool map (Record<string, ToolDecision>) for batch.
              let decision: ToolDecision | undefined;
              if (typeof rawDecision === "object" && rawDecision !== null) {
                decision = (rawDecision as Record<string, ToolDecision>)[request.id];
              } else {
                decision = rawDecision as ToolDecision | undefined;
              }
              if (decision === "approve") {
                initialStatus = "approved";
              } else if (decision === "reject") {
                initialStatus = "rejected";
              } else {
                initialStatus = "pending_approval";
              }
            }
            // Extract subagent_session_id from ProcessedToolCallData in metadata
            const toolCallData = msgMetadata?.toolCallData as ProcessedToolCallData[] | undefined;
            const matchingCallData = toolCallData?.find(tc => tc.id === request.id);

            // For agent tools, resolve the subagent session ID.
            let subagentSessionId: string | undefined = matchingCallData?.subagent_session_id;
            if (!subagentSessionId && isAgentToolName(request.name)) {
              const fcDataPart = message.parts?.find(p =>
                p.kind === "data" && p.metadata &&
                getMetadataValue<string>(p.metadata as Record<string, unknown>, "type") === "function_call" &&
                (p.data as Record<string, unknown>)?.id === request.id
              );
              subagentSessionId = fcDataPart?.metadata
                ? getMetadataValue<string>(fcDataPart.metadata as Record<string, unknown>, "subagent_session_id")
                : undefined;
            }

            newToolCalls.set(request.id, {
              id: request.id,
              call: request,
              status: initialStatus,
              subagentSessionId,
            });
          }
        }
      }
    }

    // Second pass: update with execution results.
    // "approved" / "rejected" are terminal HITL states — don't override them.
    const isHitlTerminal = (s: ToolCallStatus) => s === "approved" || s === "rejected";

    for (const message of allMessages) {
      if (isToolCallExecutionMessage(message)) {
        const results = extractToolCallResults(message);
        for (const result of results) {
          if (result.call_id && newToolCalls.has(result.call_id)) {
            const existingCall = newToolCalls.get(result.call_id)!;
            existingCall.result = {
              content: result.content,
              is_error: result.is_error,
              rawResult: result.raw_result,
            };
            if (result.subagent_session_id && !existingCall.subagentSessionId) {
              // Only set from function_response if the 1st pass (function_call
              // metadata) didn't already provide it.
              existingCall.subagentSessionId = result.subagent_session_id;
            }
            if (!isHitlTerminal(existingCall.status)) {
              existingCall.status = "executing";
            }
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

  // If no tool calls to display for this message, return null
  const currentDisplayableCalls = Array.from(toolCalls.values()).filter(call => ownedCallIds.has(call.id));
  if (currentDisplayableCalls.length === 0) return null;

  const tokenStats = (currentMessage.metadata as Record<string, unknown> | undefined)?.tokenStats as TokenStats | undefined;

  return (
    <div className="w-full min-w-0 max-w-full space-y-2 overflow-hidden">
      {currentDisplayableCalls.map(toolCall => {
        // Determine effective status: use local pending decision for optimistic UI
        const localDecision = pendingDecisions?.[toolCall.id];
        const effectiveStatus: ToolCallStatus = localDecision
          ? (localDecision === "approve" ? "approved" : "rejected")
          : toolCall.status;
        // Hide approve/reject buttons if a local decision was already made
        const showButtons = toolCall.status === "pending_approval" && !localDecision;
        // Tool has been decided locally but batch may not be submitted yet
        const isDecided = !!localDecision;

        // For approval requests, always use ToolDisplay (which has approve/reject buttons),
        // even when the tool name contains __NS__ (agent name pattern).
        // AgentCallDisplay has no concept of pending_approval and won't show buttons.
        const msgMeta = currentMessage.metadata as ADKMetadata;
        const isApprovalRequest = msgMeta?.originalType === "ToolApprovalRequest";
        const subagentName = isApprovalRequest ? (msgMeta?.subagentName as string | undefined) : undefined;
        return (!isApprovalRequest && isAgentToolName(toolCall.call.name)) ? (
          <AgentCallDisplay
            key={toolCall.id}
            call={toolCall.call}
            result={toolCall.result}
            status={effectiveStatus === "pending_approval" ? "requested" : effectiveStatus as AgentCallStatus}
            isError={toolCall.result?.is_error}
            tokenStats={tokenStats}
            subagentSessionId={toolCall.subagentSessionId}
          />
        ) : (
          <ToolDisplay
            key={toolCall.id}
            call={toolCall.call}
            result={toolCall.result}
            status={effectiveStatus}
            isError={toolCall.result?.is_error}
            isDecided={isDecided}
            subagentName={subagentName}
            onApprove={showButtons && onApprove ? () => onApprove(toolCall.id) : undefined}
            onReject={showButtons && onReject ? (reason?: string) => onReject(toolCall.id, reason) : undefined}
            tokenStats={tokenStats}
            mcpApp={getMcpAppForTool?.(toolCall.call.name)}
            onMcpAppSendMessage={onMcpAppSendMessage}
          />
        );
      })}
    </div>
  );
};

export default ToolCallDisplay;
