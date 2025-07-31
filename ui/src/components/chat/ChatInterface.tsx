"use client";

import type React from "react";
import { useState, useRef, useEffect } from "react";
import { ArrowBigUp, X, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { Session, AgentMessageConfig } from "@/types/datamodel";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import StreamingMessage from "./StreamingMessage";
import TokenStatsDisplay from "./TokenStats";
import { TokenStats } from "@/lib/types";
import StatusDisplay from "./StatusDisplay";
import { createSession, getSessionMessages, checkSessionExists } from "@/app/actions/sessions";
import { getCurrentUserId } from "@/app/actions/utils";
import { toast } from "sonner";
import { useRouter } from "next/navigation";
import { createMessageHandlers } from "@/lib/messageHandlers";
import { kagentA2AClient } from "@/lib/a2aClient";
import { v4 as uuidv4 } from "uuid";
import { getStatusPlaceholder } from "@/lib/statusUtils";

export type ChatStatus = "ready" | "thinking" | "error" | "submitted" | "working" | "input_required" | "auth_required" | "processing_tools" | "generating_response";

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
  const [messages, setMessages] = useState<AgentMessageConfig[]>([]);
  const [streamingContent, setStreamingContent] = useState<string>("");
  const [isStreaming, setIsStreaming] = useState<boolean>(false);
  const abortControllerRef = useRef<AbortController | null>(null);
  const isFirstAssistantChunkRef = useRef(true);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [sessionNotFound, setSessionNotFound] = useState<boolean>(false);
  const isCreatingSessionRef = useRef<boolean>(false);
  const [isFirstMessage, setIsFirstMessage] = useState<boolean>(!sessionId);

  const { handleMessageEvent } = createMessageHandlers({
    setMessages,
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
      
      // Skip completely if this is a first message session creation flow
      if (isFirstMessage || isCreatingSessionRef.current) {
        return;
      }

      // Skip loading state for empty sessionId (new chat)
      if (!sessionId) {
        setIsLoading(false);
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

        const messagesResponse = await getSessionMessages(sessionId);
        if (messagesResponse.error) {
          toast.error("Failed to load messages");
          setIsLoading(false);
          return;
        }
        if (!messagesResponse.data || messagesResponse?.data?.length === 0) {
          setMessages([])
        }
        else {
          setMessages(messagesResponse.data);
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
  }, [messages, streamingContent]);



  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!currentInputMessage.trim() || !selectedAgentName || !selectedNamespace) {
      return;
    }

    const userMessageText = currentInputMessage;
    // Instantly show the user's message
    setMessages(prevMessages => [...prevMessages, {
      type: "TextMessage",
      content: userMessageText,
      source: "user"
    }]);
    setCurrentInputMessage("");
    setChatStatus("thinking");

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

      abortControllerRef.current = new AbortController();

      try {
        const messageId = uuidv4();
        const a2aMessage = kagentA2AClient.createA2AMessage(userMessageText, messageId);
        const sendParams = kagentA2AClient.createMessageSendParams(a2aMessage);
        const stream = await kagentA2AClient.sendMessageStream(selectedNamespace, selectedAgentName, sendParams);

        let lastEventTime = Date.now();
        const streamTimeout = 60000;

        for await (const event of stream) {
          lastEventTime = Date.now();

          try {
            handleMessageEvent(event as AgentMessageConfig);
          } catch (error) {
            console.error("❌ Event that caused error:", event);
          }

          // Check if we should stop streaming due to cancellation
          if (abortControllerRef.current?.signal.aborted) {
            break;
          }

          // Timeout check (in case stream hangs)
          if (Date.now() - lastEventTime > streamTimeout) {
            console.warn("⏰ Stream timeout - no events received for 30 seconds");
            break;
          }
        }
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } catch (error: any) {
        if (error.name === "AbortError") {
          toast.info("Request cancelled");
          setChatStatus("ready");
        } else {
          toast.error(`Streaming failed: ${error.message}`);
          setChatStatus("error");
          setCurrentInputMessage(userMessageText);
        }

        // Clean up streaming state
        setIsStreaming(false);
        setStreamingContent("");
      } finally {
        setChatStatus("ready");
        abortControllerRef.current = null;
      }
    } catch (error) {
      console.error("Error sending message or creating session:", error);
      toast.error("Error sending message or creating session");
      setChatStatus("error");
      setCurrentInputMessage(userMessageText);
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
            ) : messages.length === 0 && !isStreaming ? (
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
                {messages.map((message, index) => {
                  return <ChatMessage key={index} message={message} allMessages={messages} />
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
