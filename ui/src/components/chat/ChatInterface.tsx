"use client";

import type React from "react";
import { useState, useRef, useEffect } from "react";
import { ArrowBigUp, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { Session, AgentMessageConfig, TextMessageConfig } from "@/types/datamodel";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import StreamingMessage from "./StreamingMessage";
import TokenStatsDisplay, { calculateTokenStats } from "./TokenStats";
import { TokenStats } from "@/lib/types";
import StatusDisplay from "./StatusDisplay";
import { createSession } from "@/app/actions/sessions";
import { getCurrentUserId } from "@/app/actions/utils";
import { messageUtils } from "@/lib/utils";
import { toast } from "sonner";

export type ChatStatus = "ready" | "thinking" | "error";

interface ChatInterfaceProps {
  selectedAgentId: number;
  selectedSession?: Session | null;
}

export default function ChatInterface({ selectedAgentId, selectedSession }: ChatInterfaceProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [currentInputMessage, setCurrentInputMessage] = useState("");
  const [tokenStats, setTokenStats] = useState<TokenStats>({
    total: 0,
    input: 0,
    output: 0,
  });

  const [chatStatus, setChatStatus] = useState<ChatStatus>("ready");

  const [session, setSession] = useState<Session | null>(null);
  const [messages, setMessages] = useState<AgentMessageConfig[]>([]);
  const [streamingContent, setStreamingContent] = useState<string>("");
  const [isStreaming, setIsStreaming] = useState<boolean>(false);
  const abortControllerRef = useRef<AbortController | null>(null);
  const isFirstAssistantChunkRef = useRef(true);

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
    if (!currentInputMessage.trim() || !selectedAgentId) {
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
      let currentSession = session;
      if (!currentSession) {
        // TODO: new session should be created when the page is visited and user should be redirected to /agents/id/chat/session_id
        const newSessionResponse = await createSession({
          user_id: await getCurrentUserId(),
          team_id: String(selectedAgentId),
          name: "New Chat",
        });

        if (newSessionResponse.success && newSessionResponse.data) {
          setSession(newSessionResponse.data);
          currentSession = newSessionResponse.data;
        } else {
          toast.error("Failed to create session");
          setChatStatus("error");
          setCurrentInputMessage(userMessageText);
          return;
        }
      }

      if (currentSession && currentSession.id) {
        abortControllerRef.current = new AbortController();

        try {
          const response = await fetch(
            `/api/sessions/${currentSession.id}/invoke/stream`,
            {
              method: 'POST',
              headers: {
                'Content-Type': 'text/plain',
              },
              body: userMessageText,
              signal: abortControllerRef.current.signal,
            }
          );

          if (!response.ok) {
            let errorText = `HTTP error! status: ${response.status}`;
            try {
              const resText = await response.text();
              if (resText) errorText = `${errorText} - ${resText}`;
            } catch (e) { /* ignore */ }
            toast.error(errorText);
            throw new Error(errorText);
          }

          if (!response.body) {
            toast.error("Response body is null");
            throw new Error("Response body is null");
          }

          const reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = "";

          while (true) {
            const { value, done } = await reader.read();

            if (done) {
              break;
            }

            if (!value) {
              continue;
            }

            buffer += decoder.decode(value, { stream: true });
            
            let eventName = 'message';
            let eventData = '';
            
            // Process all complete lines in buffer
            const lines = buffer.split('\n');
            buffer = lines.pop() || ''; // Keep the last incomplete line in buffer
            
            for (const line of lines) {
              if (line.trim() === '') continue;
              
              if (line.includes('event:')) {
                eventName = line.substring(line.indexOf('event:') + 6).trim();
              } else if (line.includes('data:')) {
                eventData = line.substring(line.indexOf('data:') + 5).trim();
                
                if (eventData) {
                  try {
                    const eventDataJson = JSON.parse(eventData) as AgentMessageConfig;

                    if (messageUtils.isStreamingMessage(eventDataJson)) {
                      // Set the streaming flag to true and concatenate the content
                      setIsStreaming(true);
                      setStreamingContent(prev => prev + eventDataJson.content);
                    } else if (messageUtils.isTextMessageContent(eventDataJson)) {
                      // The model usage is sent within the TextMessage, after the streaming is ocmplete
                      setTokenStats(prev => calculateTokenStats(prev, eventDataJson as TextMessageConfig));
                      setIsStreaming(false);
                      setStreamingContent("");
                      if (eventDataJson.source !== "user") {
                        // We don't want to add the user's message to the messages array (again), because 
                        // we already added it when the user sent the message.
                        setMessages(prevMessages => [...prevMessages, eventDataJson]);
                      }
                    }
                    else {
                      setIsStreaming(false);
                      setStreamingContent("");
                      setMessages(prevMessages => [...prevMessages, eventDataJson]);
                    }
                  } catch (error) {
                    toast.error("Error parsing event data");
                    console.error("Error parsing event data:", error, eventData);
                  }
                }
              }
            }
          }
        } catch (error: any) {
          if (error.name === "AbortError") {
            toast.error("Fetch aborted");
          } else {
            toast.error("Streaming failed");
            setChatStatus("error");
            setCurrentInputMessage(userMessageText);
          }
        } finally {
          setChatStatus("ready");
          abortControllerRef.current = null;
        }
      } else {
        toast.error("Session ID is undefined");
        setChatStatus("error");
        setCurrentInputMessage(userMessageText);
      }
    } catch (error) {
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
    setChatStatus("ready");
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      if (currentInputMessage.trim() && selectedAgentId) {
        handleSendMessage(e);
      }
    }
  };

  return (
    <div className="w-full h-screen flex flex-col justify-center min-w-full items-center transition-all duration-300 ease-in-out">
      <div className="flex-1 w-full overflow-hidden relative">
        <ScrollArea ref={containerRef} className="w-full h-full py-12">
          <div className="flex flex-col space-y-5 px-4">
            {messages.map((message, index) => {
              return <ChatMessage key={index} message={message} allMessages={messages} />
            })}
            {isStreaming && (
              <StreamingMessage 
                content={streamingContent}
              />
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
            placeholder={"Send a message..."}
            onKeyDown={handleKeyDown}
            className={`min-h-[100px] border-0 shadow-none p-0 focus-visible:ring-0 resize-none ${chatStatus === "thinking" ? "opacity-50 cursor-not-allowed" : ""}`}
            disabled={chatStatus === "thinking"}
          />

          <div className="flex items-center justify-end gap-2 mt-4">
              <Button type="submit" className={""} disabled={!currentInputMessage.trim() || chatStatus === "thinking"}>
                Send
                <ArrowBigUp className="h-4 w-4 ml-2" />
              </Button>
          
            {chatStatus === "thinking" && (
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
