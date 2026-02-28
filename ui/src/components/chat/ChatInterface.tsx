"use client";

import type React from "react";
import { useState, useRef, useEffect } from "react";
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
import TokenStatsDisplay from "./TokenStats";
import type { TokenStats, Session, ChatStatus } from "@/types";
import StatusDisplay from "./StatusDisplay";
import { createSession, getSessionTasks, checkSessionExists } from "@/app/actions/sessions";
import { getCurrentUserId } from "@/app/actions/utils";
import { toast } from "sonner";
import { useRouter } from "next/navigation";
import { createMessageHandlers, extractMessagesFromTasks, extractApprovalMessagesFromTasks, extractTokenStatsFromTasks, createMessage, ADKMetadata, ProcessedToolCallData } from "@/lib/messageHandlers";
import { kagentA2AClient } from "@/lib/a2aClient";
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
  const router = useRouter();
  const containerRef = useRef<HTMLDivElement>(null);
  const [currentInputMessage, setCurrentInputMessage] = useState("");
  const [tokenStats, setTokenStats] = useState<TokenStats>({
    total: 0,
    input: 0,
    output: 0,
  });

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
  const [pendingDecisions, setPendingDecisions] = useState<Record<string, "approve" | "deny">>({});
  const pendingDecisionsRef = useRef<Record<string, "approve" | "deny">>({});

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

  const { handleMessageEvent } = createMessageHandlers({
    setMessages: setStreamingMessages,
    setIsStreaming,
    setStreamingContent,
    setTokenStats,
    setChatStatus,
    agentContext: {
      namespace: selectedNamespace,
      agentName: selectedAgentName
    }
  });

  useEffect(() => {
    async function initializeChat() {
      setTokenStats({ total: 0, input: 0, output: 0 });
      setStreamingMessages([]);
      setPendingDecisions({});
      pendingDecisionsRef.current = {};

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
          setTokenStats({ total: 0, input: 0, output: 0 });
        }
        else {
          const extractedMessages = extractMessagesFromTasks(messagesResponse.data);
          const extractedTokenStats = extractTokenStatsFromTasks(messagesResponse.data);

          // Resolved approvals are already inline in extractedMessages (with
          // approved/rejected badges). Only pending approvals need appending.
          const { messages: pendingApprovalMessages, hasPendingApproval } = extractApprovalMessagesFromTasks(messagesResponse.data);

          setStoredMessages(
            hasPendingApproval
              ? [...extractedMessages, ...pendingApprovalMessages]
              : extractedMessages
          );
          setTokenStats(extractedTokenStats);

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
            user_id: await getCurrentUserId(),
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
    },
  ) => {
    abortControllerRef.current = new AbortController();
    isFirstAssistantChunkRef.current = true;

    try {
      const sendParams = { message: a2aMessage, metadata: {} };
      const stream = await kagentA2AClient.sendMessageStream(
        selectedNamespace,
        selectedAgentName,
        sendParams,
        abortControllerRef.current?.signal
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
    const stampDecision = (msgs: Message[]) => msgs.map(m => {
      const meta = m.metadata as Record<string, unknown> | undefined;
      if (meta?.originalType === "ToolApprovalRequest" && !meta.approvalDecision) {
        const toolCallData = meta?.toolCallData as Array<{ id?: string }> | undefined;
        const toolId = toolCallData?.[0]?.id;
        // For uniform decisions, stamp all; for batch, stamp per-tool
        const dt = decisionData.decision_type as string;
        let resolvedDecision: string | undefined;
        if (dt === "batch") {
          const decisions = decisionData.decisions as Record<string, string>;
          resolvedDecision = toolId ? decisions[toolId] : undefined;
        } else {
          resolvedDecision = dt; // "approve" or "deny"
        }
        if (resolvedDecision) {
          return { ...m, metadata: { ...meta, approvalDecision: resolvedDecision } };
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
      onFinally: () => {
        // Ensure chat state resets after approval stream ends
        setIsStreaming(false);
        setStreamingContent("");
        setPendingDecisions({});
        pendingDecisionsRef.current = {};
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
  const submitDecisions = (decisions: Record<string, "approve" | "deny">) => {
    const values = Object.values(decisions);
    const allApprove = values.every(v => v === "approve");
    const allDeny = values.every(v => v !== "approve");

    if (allApprove) {
      // Uniform approve — no need for batch
      sendApprovalDecision(
        { decision_type: "approve" },
        "Approved",
      );
    } else if (allDeny) {
      // Uniform deny
      sendApprovalDecision(
        { decision_type: "deny" },
        "Rejected",
      );
    } else {
      // Mixed decisions — use batch mode with per-tool decisions
      sendApprovalDecision(
        { decision_type: "batch", decisions },
        `Batch decision: ${values.filter(v => v === "approve").length} approved, ${values.filter(v => v !== "approve").length} rejected`,
      );
    }
  };

  const recordDecision = (toolCallId: string, decision: "approve" | "deny") => {
    const updated = { ...pendingDecisionsRef.current, [toolCallId]: decision };
    pendingDecisionsRef.current = updated;
    setPendingDecisions(updated);

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

  const handleReject = (toolCallId: string) => {
    recordDecision(toolCallId, "deny");
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
      <div className="flex flex-col items-center justify-center w-full h-full">
        <div className="text-xl font-semibold mb-4">Session not found</div>
        <p className="text-muted-foreground mb-6">This chat session may have been deleted or does not exist.</p>
        <Button onClick={() => router.push(`/agents/${selectedNamespace}/${selectedAgentName}/chat`)}>
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
              <div className="flex items-center justify-center h-full min-h-[50vh]">
                <div className="flex flex-col items-center gap-2">
                  <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
                  <p className="text-muted-foreground text-sm">Loading your chat session...</p>
                </div>
              </div>
            ) : storedMessages.length === 0 && streamingMessages.length === 0 && !isStreaming ? (
              <div className="flex items-center justify-center h-full min-h-[50vh]">
                <div className="bg-card p-6 rounded-lg shadow-sm border max-w-md text-center">
                  <h3 className="text-lg font-medium mb-2">Start a conversation</h3>
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
                    allMessages={[...storedMessages, ...streamingMessages]}
                    agentContext={{
                      namespace: selectedNamespace,
                      agentName: selectedAgentName
                    }}
                    onApprove={handleApprove}
                    onReject={handleReject}
                    pendingDecisions={pendingDecisions}
                  />
                })}

                {/* Display streaming messages */}
                {streamingMessages.map((message, index) => {
                  return <ChatMessage
                    key={`stream-${index}`}
                    message={message}
                    allMessages={[...storedMessages, ...streamingMessages]}
                    agentContext={{
                      namespace: selectedNamespace,
                      agentName: selectedAgentName
                    }}

                    onApprove={handleApprove}
                    onReject={handleReject}
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
          <TokenStatsDisplay stats={tokenStats} />
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
