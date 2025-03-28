"use client";

import type React from "react";
import { useState, useRef, useEffect } from "react";
import { ArrowBigUp, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { Session, Run } from "@/types/datamodel";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import useChatStore from "@/lib/useChatStore";
import StreamingMessage from "./StreamingMessage";
import NoMessagesState from "./NoMessagesState";
import TokenStatsDisplay, { calculateTokenStats } from "./TokenStats";
import { TokenStats } from "@/lib/types";
import StatusDisplay from "./StatusDisplay";
import Link from "next/link";

interface ChatInterfaceProps {
  selectedAgentId: number;
  selectedSession?: Session | null;
  selectedRun?: Run | null;
}

export default function ChatInterface({ selectedAgentId, selectedRun }: ChatInterfaceProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [message, setMessage] = useState("");
  const [tokenStats, setTokenStats] = useState<TokenStats>({
    total: 0,
    input: 0,
    output: 0,
  });

  const { status, messages, run, sendUserMessage, cleanup, currentStreamingContent, currentStreamingMessage } = useChatStore();

  useEffect(() => {
    return () => {
      cleanup();
    };
  }, [cleanup]);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [messages, currentStreamingContent]);

  useEffect(() => {
    const msgs = selectedRun ? selectedRun.messages : messages;
    if (msgs.length > 0) {
      const newStats = calculateTokenStats(msgs);
      setTokenStats(newStats);
    }
  }, [messages, selectedRun]);

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!message.trim() || !selectedAgentId) {
      return;
    }

    const currentMessage = message;
    setMessage("");

    try {
      await sendUserMessage(currentMessage, String(selectedAgentId));
    } catch (error) {
      console.error("Error sending message:", error);
      setMessage(currentMessage);
    }
  };

  const handleCancel = async (e: React.FormEvent) => {
    e.preventDefault();
    cleanup();
    setMessage("");
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      if (message.trim() && selectedAgentId) {
        handleSendMessage(e);
      }
    }
  };

  // show messages from selected run if available
  const displayMessages = selectedRun ? selectedRun.messages : messages;
  const actualRun = selectedRun || run || null;
  const runStatus = actualRun?.status;
  const canSendMessage = status !== "thinking" && status !== "error" && runStatus !== "complete" && runStatus !== "error" && runStatus !== "stopped";

  // Should we show the streaming message?
  const showStreamingMessage = !selectedRun && currentStreamingContent && currentStreamingMessage;

  return (
    <div className="w-full h-screen flex flex-col justify-center min-w-full items-center transition-all duration-300 ease-in-out">
      <div className="flex-1 w-full overflow-hidden relative">
        <ScrollArea ref={containerRef} className="w-full h-full py-12">
          <div className="flex flex-col space-y-5">
            {displayMessages.length === 0 && !showStreamingMessage && <NoMessagesState agentId={selectedAgentId} />}
            {displayMessages.map((msg, index) => (
              <ChatMessage key={`${msg.run_id}-${msg.config.source}-${index}`} message={msg} run={actualRun} />
            ))}

            {showStreamingMessage && currentStreamingMessage && (
              <StreamingMessage
                key={`streaming-${currentStreamingMessage.config.source}`}
                message={{
                  ...currentStreamingMessage,
                  config: {
                    ...currentStreamingMessage.config,
                    content: currentStreamingContent,
                  },
                }}
              />
            )}
          </div>
        </ScrollArea>
      </div>

      <div className="w-full sticky bg-secondary bottom-0 md:bottom-2 rounded-none md:rounded-lg p-4 border  overflow-hidden transition-all duration-300 ease-in-out">
        <div className="flex items-center justify-between mb-4">
          <StatusDisplay chatStatus={status} errorMessage={actualRun?.error_message || selectedRun?.error_message} />
          <TokenStatsDisplay stats={tokenStats} />
        </div>

        <form onSubmit={handleSendMessage}>
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder={"Send a message..."}
            onKeyDown={handleKeyDown}
            disabled={!canSendMessage}
            className={`min-h-[100px] border-0 shadow-none p-0 focus-visible:ring-0 resize-none`}
          />

          <div className="flex items-center justify-end gap-2 mt-4">
            {canSendMessage && (
              <Button type="submit" className={""} disabled={!message.trim()}>
                Send
                <ArrowBigUp className="h-4 w-4 ml-2" />
              </Button>
            )}

            {(runStatus === "complete" || runStatus === "error" || runStatus === "stopped" || status === 'error') && (
              <Button className="bg-violet-500 hover:bg-violet-600" asChild>
                <Link href={`/agents/${selectedAgentId}/chat`}>
                Start New Chat
                </Link>
              </Button>
            )}

            {status === "thinking" && (
              <Button onClick={handleCancel} className="" variant={"destructive"} type="button">
                Cancel
                <X className="h-4 w-4 ml-2" />
              </Button>
            )}
          </div>
        </form>
      </div>
    </div>
  );
}
