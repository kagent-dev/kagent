"use client";

import type React from "react";
import { useState, useRef, useEffect, useMemo } from "react";
import { ArrowBigUp, X, Loader2, Mic, Square } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useSpeechRecognition } from "@/hooks/useSpeechRecognition";
import { Textarea } from "@/components/ui/textarea";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import StreamingMessage from "./StreamingMessage";
import SessionTokenStatsDisplay from "@/components/chat/TokenStats";
import type { TokenStats, Session, ChatStatus, ToolDecision } from "@/types";
import StatusDisplay from "./StatusDisplay";
import { createSession, getSessionTasks, checkSessionExists } from "@/app/actions/sessions";
import { waitForSandboxAgentReady } from "@/app/actions/agents";
import { toast } from "sonner";
import { useRouter } from "next/navigation";
import { createMessageHandlers, extractMessagesFromTasks, extractApprovalMessagesFromTasks, extractTokenStatsFromTasks, createMessage, ADKMetadata, ProcessedToolCallData } from "@/lib/messageHandlers";
import { kagentA2AClient } from "@/lib/a2aClient";
import { useChatRunInSandbox } from "@/components/chat/ChatAgentContext";
import { v4 as uuidv4 } from "uuid";
import { getStatusPlaceholder } from "@/lib/statusUtils";
import { Message, DataPart } from "@a2a-js/sdk";

interface ChatInterfaceProps {
  selectedAgentName: string;
  selectedNamespace: string;
  selectedSession?: Session | null;
  sessionId?: string;
}

export default function ChatInterface({ selectedAgentName, selectedNamespace, selectedSession, sessionId }: ChatInterfaceProps) {
  const runInSandbox = useChatRunInSandbox();
  const router = useRouter();
  const containerRef = useRef<HTMLDivElement>(null);
  const [currentInputMessage, setCurrentInputMessage] = useState("");

  const [chatStatus, setChatStatus] = useState<ChatStatus>("ready");

  const [session, setSession] = useState<Session | null>(selectedSession || null);
  const [storedMessages, setStoredMessages] = useState<Message[]>([]);
  const [streamingMessages, setStreamingMessages] = useState<Message[]>([]);
  const [streamingContent, setStreamingContent] = useState<string>("");
  const [isStreaming, setIsStreaming] = useState<boolean>(false);
  const abortControllerRef = useRef<AbortController | null>(null);
  const isFirstAssistantChunkRef = useRef(true);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [sessionNotFound, setSessionNotFound] = useState<boolean>(false);
  const isCreatingSessionRef = useRef<boolean>(false);
  const [isFirstMessage, setIsFirstMessage] = useState<boolean>(!sessionId);
  const [sessionStats, setSessionStats] = useState<TokenStats>({ total: 0, prompt: 0, completion: 0 });
  // Mutable ref so pendingTurnStats survives re-renders between A2A stream events
  const pendingTurnStatsRef = useRef<TokenStats | undefined>(undefined);
  const [pendingDecisions, setPendingDecisions] = useState<Record<string, ToolDecision>>({});
  const pendingDecisionsRef = useRef<Record<string, ToolDecision>>({});
  /** Per-tool rejection reasons collected as the user rejects individual tools. */
  const pendingRejectionReasonsRef = useRef<Record<string, string>>({});

  const {
    isListening,
    isSupported: isVoiceSupported,
    startListening,
    stopListening,
    error: voiceError,
  } = useSpeechRecognition({
    onResult(transcriptText) {
      setCurrentInputMessage(transcriptText);
    },
    onError(msg) {
      toast.error(msg);
    },
  });

  const agentContext = useMemo(() => ({
    namespace: selectedNamespace,
    agentName: selectedAgentName
  }), [selectedNamespace, selectedAgentName]);

  const allMessages = useMemo(() => [...storedMessages, ...streamingMessages], [storedMessages, streamingMessages]);

  const { handleMessageEvent } = useMemo(() => createMessageHandlers({
    setMessages: setStreamingMessages,
    setIsStreaming,
    setStreamingContent,
    setChatStatus,
    setSessionStats,
    pendingTurnStats: pendingTurnStatsRef,
    agentContext: {
      namespace: selectedNamespace,
      agentName: selectedAgentName
    }
  }), [selectedNamespace, selectedAgentName]);

  useEffect(() => {
    async function initializeChat() {
      setSessionStats({ total: 0, prompt: 0, completion: 0 });
      setStreamingMessages([]);
      setPendingDecisions({});
      pendingDecisionsRef.current = {};
      pendingRejectionReasonsRef.current = {};
      pendingTurnStatsRef.current = undefined;

      // Skip completely if this is a first message session creation flow
      if (isFirstMessage || isCreatingSessionRef.current) {
        return;
      }

      // Skip loading state for empty sessionId (new chat)
      if (!sessionId) {
        setIsLoading(false);
        setStoredMessages([]);
        return;
      }

      setIsLoading(true);
      setSessionNotFound(false);

      try {
        const sessionExistsResponse = await checkSessionExists(sessionId);
        if (sessionExistsResponse.error || !sessionExistsResponse.data) {
          setSessionNotFound(true);
          setIsLoading(false);
          return;
        }

        const messagesResponse = await getSessionTasks(sessionId);
        if (messagesResponse.error) {
          toast.error("Failed to load messages");
          setIsLoading(false);
          return;
        }
        if (!messagesResponse.data || messagesResponse?.data?.length === 0) {
          setStoredMessages([]);
          setSessionStats({ total: 0, prompt: 0, completion: 0 });
        }
        else {
          const extractedMessages = extractMessagesFromTasks(messagesResponse.data);
          setSessionStats(extractTokenStatsFromTasks(messagesResponse.data));

          // Resolved approvals are already inline in extractedMessages (with
          // approved/rejected badges). Only pending approvals need appending.
          const { messages: pendingApprovalMessages, hasPendingApproval } = extractApprovalMessagesFromTasks(messagesResponse.data);

          setStoredMessages(
            hasPendingApproval
              ? [...extractedMessages, ...pendingApprovalMessages]
              : extractedMessages
          );

          if (hasPendingApproval) {
            setChatStatus("input_required");
          }
        }
      } catch (error) {
        console.error("Error loading messages:", error);
        toast.error("Error loading messages");
        setSessionNotFound(true);
      }
      setIsLoading(false);
    }

    initializeChat();
  }, [sessionId, selectedAgentName, selectedNamespace, isFirstMessage]);

  useEffect(() => {
    if (containerRef.current) {
      const viewport = containerRef.current.querySelector('[data-radix-scroll-area-viewport]') as HTMLElement;
      if (viewport) {
        viewport.scrollTop = viewport.scrollHeight;
      }
    }
  }, [storedMessages, streamingMessages, streamingContent]);



  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!currentInputMessage.trim() || !selectedAgentName || !selectedNamespace) {
      return;
    }

    // Stop voice recording if active before sending
    if (isListening) {
      stopListening();
    }

    const userMessageText = currentInputMessage;
    setCurrentInputMessage("");
    setChatStatus("thinking");
    setStoredMessages(prev => [...prev, ...streamingMessages]);
    setStreamingMessages([]);
    setStreamingContent(""); // Reset streaming content for new message
    setPendingDecisions({});
    pendingDecisionsRef.current = {};
    pendingRejectionReasonsRef.current = {};
    pendingTurnStatsRef.current = undefined;

    // For new sessions or when no stored messages exist, show the user message immediately
    const userMessage: Message = {
      kind: "message",
      messageId: uuidv4(),
      role: "user",
      parts: [{
        kind: "text",
        text: userMessageText
      }],
      metadata: {
        timestamp: Date.now()
      }
    };

    // Add user message to streaming messages to show immediately
    // (will be replaced by server response that includes the user message)
    setStreamingMessages([userMessage]);

    isFirstAssistantChunkRef.current = true;

    try {
      let currentSessionId = session?.id || sessionId;

      // If there's no session, create one
      if (!currentSessionId) {
        try {
          // Set flags to prevent loading screens during first message
          isCreatingSessionRef.current = true;
          setIsFirstMessage(true);

          const newSessionResponse = await createSession({
            agent_ref: `${selectedNamespace}/${selectedAgentName}`,
            name: userMessageText.slice(0, 20) + (userMessageText.length > 20 ? "..." : ""),
          });

          if (newSessionResponse.error || !newSessionResponse.data) {
            toast.error("Failed to create session");
            setChatStatus("error");
            setCurrentInputMessage(userMessageText);
            isCreatingSessionRef.current = false;
            return;
          }

          currentSessionId = newSessionResponse.data.id;
          setSession(newSessionResponse.data);

          // Update URL without triggering navigation or component reload
          const newUrl = `/agents/${selectedNamespace}/${selectedAgentName}/chat/${currentSessionId}`;
          window.history.replaceState({}, '', newUrl);

          // Dispatch a custom event to notify that a new session was created
          // Include the full session object to avoid needing a DB reload
          const newSessionEvent = new CustomEvent('new-session-created', {
            detail: {
              agentRef: `${selectedNamespace}/${selectedAgentName}`,
              session: newSessionResponse.data
            }
          });
          window.dispatchEvent(newSessionEvent);
        } catch (error) {
          console.error("Error creating session:", error);
          toast.error("Error creating session");
          setChatStatus("error");
          setCurrentInputMessage(userMessageText);
          isCreatingSessionRef.current = false;
          return;
        }
      }

      const messageId = uuidv4();
      const a2aMessage = createMessage(userMessageText, "user", {
        messageId,
        contextId: currentSessionId,
      });

      await streamA2AMessage(a2aMessage, {
        errorLabel: "Streaming failed",
        onError: () => setCurrentInputMessage(userMessageText),
        sessionIdForWait: currentSessionId,
      });
    } catch (error) {
      console.error("Error sending message or creating session:", error);
      toast.error("Error sending message or creating session");
      setChatStatus("error");
      setCurrentInputMessage(userMessageText);
    }
  };

  /**
   * Shared streaming helper used by both handleSendMessage and
   * sendApprovalDecision.  Handles the abort controller, timeout, event loop,
   * and base cleanup.
   */
  const streamA2AMessage = async (
    a2aMessage: Message,
    opts?: {
      errorLabel?: string;
      onError?: () => void;
      onFinally?: () => void;
      /** Session id for readiness polling when React state may lag. */
      sessionIdForWait?: string;
    },
  ) => {
    abortControllerRef.current = new AbortController();
    isFirstAssistantChunkRef.current = true;

    try {
      const sid = opts?.sessionIdForWait ?? session?.id ?? sessionId;
      if (runInSandbox && !sid) {
        throw new Error("Session is required before messaging a Sandbox agent");
      }
      if (runInSandbox && sid) {
        let loadingToast: string | number | undefined;
        const slowToast = setTimeout(() => {
          loadingToast = toast.loading("Starting sandbox workload…");
        }, 600);
        try {
          const ready = await waitForSandboxAgentReady(selectedAgentName, selectedNamespace);
          clearTimeout(slowToast);
          if (loadingToast !== undefined) toast.dismiss(loadingToast);
          if (!ready.ok) {
            throw new Error(ready.error ?? "Sandbox workload not ready");
          }
        } catch (waitErr) {
          clearTimeout(slowToast);
          if (loadingToast !== undefined) toast.dismiss(loadingToast);
          throw waitErr;
        }
      }
      isCreatingSessionRef.current = false;
      const sendParams = { message: a2aMessage, metadata: {} };
      const stream = await kagentA2AClient.sendMessageStream(
        selectedNamespace,
        selectedAgentName,
        sendParams,
        abortControllerRef.current?.signal,
        runInSandbox
      );

      let timeoutTimer: NodeJS.Timeout | null = null;
      let streamActive = true;
      const streamTimeout = 600000; // 10 minutes

      const handleTimeout = () => {
        if (streamActive) {
          console.error("⏰ Stream timeout - no events received for 10 minutes");
          toast.error("⏰ Stream timed out - no events received for 10 minutes");
          streamActive = false;
          if (abortControllerRef.current) abortControllerRef.current.abort();
        }
      };

      const startTimeout = () => {
        if (timeoutTimer) clearTimeout(timeoutTimer);
        timeoutTimer = setTimeout(handleTimeout, streamTimeout);
      };
      startTimeout();

      try {
        for await (const event of stream) {
          startTimeout();
          try {
            handleMessageEvent(event);
          } catch (error) {
            console.error(`❌ Error handling event: ${error}`);
          }
          if (abortControllerRef.current?.signal.aborted) {
            streamActive = false;
            break;
          }
        }
      } finally {
        streamActive = false;
        if (timeoutTimer) clearTimeout(timeoutTimer);
      }
    } catch (error: unknown) {
      if (error instanceof Error && error.name === "AbortError") {
        setChatStatus("ready");
      } else {
        toast.error(`${opts?.errorLabel || "Request failed"}: ${error instanceof Error ? error.message : "Unknown error"}`);
        setChatStatus("error");
        opts?.onError?.();
      }
      setIsStreaming(false);
      setStreamingContent("");
    } finally {
      abortControllerRef.current = null;
      opts?.onFinally?.();
    }
  };

  const handleCancel = (e: React.FormEvent) => {
    e.preventDefault();

    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    setIsStreaming(false);
    setStreamingContent("");
    setChatStatus("ready");
    toast.error("Request cancelled");
  };

  // Collect all pending tool call IDs from ToolApprovalRequest messages
  const getPendingApprovalToolIds = (): { toolIds: string[]; taskId: string | undefined } => {
    const toolIds: string[] = [];
    let taskId: string | undefined;
    const allCurrentMessages = [...storedMessages, ...streamingMessages];
    for (const msg of allCurrentMessages) {
      const meta = msg.metadata as ADKMetadata | undefined;
      if (meta?.originalType !== "ToolApprovalRequest") continue;
      // Skip approval messages that already have a decision (from previous cycles)
      if (meta?.approvalDecision) continue;
      if (!taskId) taskId = msg.taskId;
      const toolCallData = meta.toolCallData as ProcessedToolCallData[] | undefined;
      if (toolCallData) {
        for (const tc of toolCallData) {
          if (tc.id) toolIds.push(tc.id);
        }
      }
    }
    return { toolIds, taskId };
  };

  const sendApprovalDecision = async (
    decisionData: Record<string, unknown>,
    displayText: string,
  ) => {
    const currentSessionId = session?.id || sessionId;
    setChatStatus("thinking");
    setStreamingContent("");

    // Find the taskId from the pending approval message so the A2A framework
    // reuses the existing task instead of creating a new one.
    const { taskId: approvalTaskId } = getPendingApprovalToolIds();

    // Stamp approvalDecision on the current pending approval messages so they
    // are excluded from getPendingApprovalToolIds on future HITL cycles.
    // approvalDecision is either a uniform ToolDecision or a per-tool map
    // (Record<string, ToolDecision>) for batch decisions.
    const stampDecision = (msgs: Message[]) => msgs.map(m => {
      const meta = m.metadata as Record<string, unknown> | undefined;
      if (meta?.originalType === "ToolApprovalRequest" && !meta.approvalDecision) {
        const dt = decisionData.decision_type as string;
        if (dt === "batch") {
          // Store the per-tool decisions map so ToolCallDisplay can resolve
          // each inner tool independently.
          const decisions = decisionData.decisions as Record<string, ToolDecision>;
          return { ...m, metadata: { ...meta, approvalDecision: decisions } };
        } else {
          return { ...m, metadata: { ...meta, approvalDecision: dt as ToolDecision } };
        }
      }
      return m;
    });
    setStreamingMessages(stampDecision);
    setStoredMessages(stampDecision);

    const messageId = uuidv4();
    const a2aMessage: Message = {
      kind: "message",
      messageId,
      role: "user",
      parts: [
        { kind: "data", data: decisionData, metadata: {} } as DataPart,
        { kind: "text", text: displayText },
      ],
      contextId: currentSessionId,
      taskId: approvalTaskId,
      metadata: {
        timestamp: Date.now(),
      },
    };

    await streamA2AMessage(a2aMessage, {
      errorLabel: "Approval failed",
      sessionIdForWait: currentSessionId,
      onFinally: () => {
        // Ensure chat state resets after approval stream ends
        setIsStreaming(false);
        setStreamingContent("");
        setPendingDecisions({});
        pendingDecisionsRef.current = {};
        pendingRejectionReasonsRef.current = {};
        // Only reset "thinking" → "ready".  Do NOT reset "input_required" —
        // handleMessageEvent may have already set it for the next HITL cycle
        // during this same stream.
        setChatStatus(prev => prev === "thinking" ? "ready" : prev);
      },
    });
  };

  // Submit all collected decisions to the backend. Called when every pending
  // tool has a decision recorded in `pendingDecisions`, or immediately for
  // "approve all" / uniform decisions.
  const submitDecisions = (decisions: Record<string, ToolDecision>) => {
    const values = Object.values(decisions);
    const allApprove = values.every(v => v === "approve");
    const allReject = values.every(v => v !== "approve");
    const reasons = pendingRejectionReasonsRef.current;

    if (allApprove) {
      // Uniform approve — no need for batch
      sendApprovalDecision(
        { decision_type: "approve" },
        "Approved",
      );
    } else if (allReject && Object.values(reasons).length === 0) {
      // Uniform reject without reason, otherwise fall through to batch
      sendApprovalDecision(
        { decision_type: "reject" },
        "Rejected",
      );
    } else {
      // Mixed decisions — use batch mode with per-tool decisions.
      // For subagent HITL the keys are inner subagent tool IDs; the backend
      // detects this via hitl_parts in the pending confirmation payload and
      // forwards the batch to the subagent.
      const decisionData: Record<string, unknown> = { decision_type: "batch", decisions };
      // Include per-tool rejection reasons for denied tools (if any)
      const rejectedReasons: Record<string, string> = {};
      for (const [toolId, decision] of Object.entries(decisions)) {
        if (decision === "reject" && reasons[toolId]) {
          rejectedReasons[toolId] = reasons[toolId];
        }
      }
      if (Object.keys(rejectedReasons).length > 0) {
        decisionData.rejection_reasons = rejectedReasons;
      }
      sendApprovalDecision(
        decisionData,
        `Batch decision: ${values.filter(v => v === "approve").length} approved, ${values.filter(v => v !== "approve").length} rejected`,
      );
    }
  };

  const recordDecision = (toolCallId: string, decision: ToolDecision, reason?: string) => {
    const updated = { ...pendingDecisionsRef.current, [toolCallId]: decision };
    pendingDecisionsRef.current = updated;
    setPendingDecisions(updated);

    // Track rejection reason (if any)
    if (decision === "reject" && reason) {
      const updatedReasons = { ...pendingRejectionReasonsRef.current, [toolCallId]: reason };
      pendingRejectionReasonsRef.current = updatedReasons;
    }

    // Check if all pending tools now have a decision
    const { toolIds } = getPendingApprovalToolIds();
    if (toolIds.length > 0 && toolIds.every(id => id in updated)) {
      submitDecisions(updated);
    } else if (toolIds.length === 0) {
      submitDecisions(updated);
    }
  };

  const handleApprove = (toolCallId: string) => {
    recordDecision(toolCallId, "approve");
  };

  const handleReject = (toolCallId: string, reason?: string) => {
    recordDecision(toolCallId, "reject", reason);
  };

  /**
   * Handle ask_user answers submitted by the user. Sends an "approve" decision
   * with the answers payload attached, routed to the pending ask_user task.
   */
  const handleAskUserSubmit = (answers: Array<{ answer: string[] }>) => {
    const currentSessionId = session?.id || sessionId;
    setChatStatus("thinking");
    setStreamingContent("");

    // Find the taskId from the pending AskUserRequest message
    let askUserTaskId: string | undefined;
    const allCurrentMessages = [...storedMessages, ...streamingMessages];
    for (const msg of allCurrentMessages) {
      const meta = msg.metadata as ADKMetadata | undefined;
      if (meta?.originalType === "AskUserRequest" && !meta?.approvalDecision) {
        askUserTaskId = msg.taskId;
        break;
      }
    }

    // Stamp the ask-user message as resolved so we don't show the form again
    const stampAskUser = (msgs: Message[]) => msgs.map(m => {
      const meta = m.metadata as Record<string, unknown> | undefined;
      if (meta?.originalType === "AskUserRequest" && !meta.approvalDecision) {
        return { ...m, metadata: { ...meta, approvalDecision: "approve", askUserAnswers: answers } };
      }
      return m;
    });
    setStreamingMessages(stampAskUser);
    setStoredMessages(stampAskUser);

    const messageId = uuidv4();
    const a2aMessage: Message = {
      kind: "message",
      messageId,
      role: "user",
      parts: [
        {
          kind: "data",
          data: { decision_type: "approve", ask_user_answers: answers },
          metadata: {},
        } as DataPart,
        { kind: "text", text: "Answered questions" },
      ],
      contextId: currentSessionId,
      taskId: askUserTaskId,
      metadata: { timestamp: Date.now() },
    };

    streamA2AMessage(a2aMessage, {
      errorLabel: "Ask user response failed",
      sessionIdForWait: currentSessionId,
      onFinally: () => {
        setIsStreaming(false);
        setStreamingContent("");
        setChatStatus(prev => prev === "thinking" ? "ready" : prev);
      },
    });
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      if (currentInputMessage.trim() && selectedAgentName && selectedNamespace && chatStatus === "ready") {
        handleSendMessage(e);
      }
    }
  };

  if (sessionNotFound) {
    return (
      <div className="flex h-full w-full flex-col items-center justify-center p-4 text-center">
        <h2 className="mb-4 text-xl font-semibold">Session not found</h2>
        <p className="mb-6 text-muted-foreground">This chat session may have been deleted or does not exist.</p>
        <Button
          type="button"
          onClick={() => router.push(`/agents/${selectedNamespace}/${selectedAgentName}/chat`)}
        >
          Start a new chat
        </Button>
      </div>
    );
  }
  return (
    <div className="w-full h-screen flex flex-col justify-center min-w-full items-center transition-all duration-300 ease-in-out">
      <div className="flex-1 w-full overflow-hidden relative">
        <ScrollArea ref={containerRef} className="w-full h-full py-12">
          <div className="flex flex-col space-y-5 px-4">
            {/* Never show loading for first message/new session */}
            {isLoading && sessionId && !isFirstMessage && !isCreatingSessionRef.current ? (
              <div
                className="flex h-full min-h-[50vh] items-center justify-center"
                role="status"
                aria-live="polite"
                aria-busy="true"
              >
                <div className="flex flex-col items-center gap-2">
                  <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" aria-hidden />
                  <p className="text-sm text-muted-foreground">Loading your chat session…</p>
                </div>
              </div>
            ) : storedMessages.length === 0 && streamingMessages.length === 0 && !isStreaming ? (
              <div className="flex items-center justify-center h-full min-h-[50vh]">
                <div className="max-w-md rounded-lg border bg-card p-6 text-center shadow-sm">
                  <h2 className="mb-2 text-lg font-medium">Start a conversation</h2>
                  <p className="text-muted-foreground">
                    To begin chatting with the agent, type your message in the input box below.
                  </p>
                </div>
              </div>
            ) : (
              <>
                {/* Display stored messages from session */}
                {storedMessages.map((message, index) => {
                  return <ChatMessage
                    key={`stored-${index}`}
                    message={message}
                    allMessages={allMessages}
                    agentContext={agentContext}
                    onApprove={handleApprove}
                    onReject={handleReject}
                    onAskUserSubmit={handleAskUserSubmit}
                    pendingDecisions={pendingDecisions}
                  />
                })}

                {/* Display streaming messages */}
                {streamingMessages.map((message, index) => {
                  return <ChatMessage
                    key={`stream-${index}`}
                    message={message}
                    allMessages={allMessages}
                    agentContext={agentContext}
                    onApprove={handleApprove}
                    onReject={handleReject}
                    onAskUserSubmit={handleAskUserSubmit}
                    pendingDecisions={pendingDecisions}
                  />
                })}

                {isStreaming && (
                  <StreamingMessage
                    content={streamingContent}
                  />
                )}
              </>
            )}
          </div>
        </ScrollArea>
      </div>

      <div className="w-full sticky bg-secondary bottom-0 md:bottom-2 rounded-none md:rounded-lg p-4 border  overflow-hidden transition-all duration-300 ease-in-out">
        <div className="flex items-center justify-between mb-4">
          <StatusDisplay chatStatus={chatStatus} />
          {sessionStats.total > 0 && <SessionTokenStatsDisplay stats={sessionStats} />}
        </div>

        <form onSubmit={handleSendMessage}>
          <Textarea
            value={currentInputMessage}
            onChange={(e) => setCurrentInputMessage(e.target.value)}
            placeholder={getStatusPlaceholder(chatStatus)}
            onKeyDown={handleKeyDown}
            className={`min-h-[100px] border-0 shadow-none p-0 focus-visible:ring-0 resize-none ${chatStatus !== "ready" ? "opacity-50 cursor-not-allowed" : ""}`}
            disabled={chatStatus !== "ready"}
          />

          <div className="flex items-center justify-end gap-2 mt-4">
            {isVoiceSupported && (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      variant={isListening ? "destructive" : "default"}
                      size="icon"
                      onClick={isListening ? stopListening : startListening}
                      disabled={chatStatus !== "ready"}
                      className={isListening ? "animate-pulse" : ""}
                      aria-label={isListening ? "Stop listening" : "Voice input"}
                    >
                      {isListening ? (
                        <Square className="h-4 w-4" aria-hidden />
                      ) : (
                        <Mic className="h-4 w-4" aria-hidden />
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="top">
                    {voiceError
                      ? voiceError
                      : isListening
                        ? "Stop listening"
                        : "Voice input — click and speak"}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
            <Button type="submit" className={""} disabled={!currentInputMessage.trim() || chatStatus !== "ready"}>
              Send
              <ArrowBigUp className="h-4 w-4 ml-2" />
            </Button>
            {chatStatus !== "ready" && chatStatus !== "error" && (
              <Button type="button" variant="outline" onClick={handleCancel}>
                <X className="h-4 w-4 mr-2" /> Cancel
              </Button>
            )}
          </div>
        </form>
      </div>
    </div>
  );
}
