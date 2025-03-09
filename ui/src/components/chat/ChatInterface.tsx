"use client";

import type React from "react";
import { useState, useRef, useEffect } from "react";
import { ArrowLeft, ArrowRightFromLine, ArrowBigUp, AlertTriangle, CheckCircle, MessageSquare, StopCircle, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { Message, RunStatus, Session, Run } from "@/types/datamodel";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import useChatStore from "@/lib/useChatStore";
import { ChatStatus } from "@/lib/ws";
import StreamingMessage from "./StreamingMessage";
import NoMessagesState from "./NoMessagesState";

interface TokenStats {
  total: number;
  input: number;
  output: number;
}

function calculateTokenStats(messages: Message[]): TokenStats {
  return messages.reduce(
    (stats, message) => {
      const usage = message.config?.models_usage;
      if (usage) {
        return {
          total: stats.total + (usage.prompt_tokens + usage.completion_tokens),
          input: stats.input + usage.prompt_tokens,
          output: stats.output + usage.completion_tokens,
        };
      }
      return stats;
    },
    { total: 0, input: 0, output: 0 }
  );
}

interface ChatInterfaceProps {
  selectedTeamId: string;
  selectedSession?: Session | null;
  selectedRun?: Run | null;
  onNewSession?: () => void;
}

export default function ChatInterface({ selectedTeamId, selectedRun, onNewSession }: ChatInterfaceProps) {
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

    if (!message.trim() || !selectedTeamId) {
      return;
    }

    const currentMessage = message;
    setMessage("");

    try {
      await sendUserMessage(currentMessage, Number(selectedTeamId));
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

  const getStatusDisplay = (chatStatus: ChatStatus, runStatus?: RunStatus) => {
    switch (chatStatus) {
      case "error":
        return (
          <div className=" text-xs justify-center items-center flex">
            <AlertTriangle size={16} className="mr-2 text-red-500" />
            {run?.error_message || "An error occurred"}
          </div>
        );

      case "ready":
      default:
        // If run status is available and indicates a completed state, show that instead
        if (runStatus) {
          switch (runStatus) {
            case "complete":
              return (
                <div className=" text-xs justify-center items-center flex">
                  <CheckCircle size={16} className="mr-2 text-green-500" />
                  Task completed
                </div>
              );
            case "error":
            case "timeout":
              return (
                <div className=" text-xs justify-center items-center flex">
                  <AlertTriangle size={16} className="mr-2 text-red-500" />
                  {run?.error_message || "An error occurred"}
                </div>
              );
            case "stopped":
              return (
                <div className=" text-xs justify-center items-center flex">
                  <StopCircle size={16} className="mr-2 text-orange-500" />
                  Task was stopped
                </div>
              );
            default:
              return (
                <div className=" text-xs justify-center items-center flex">
                  <MessageSquare size={16} className="mr-2" />
                  Ready
                </div>
              );
          }
        }

        return (
          <div className=" text-xs justify-center items-center flex">
            <MessageSquare size={16} className="mr-2 text-white" />
            Ready
          </div>
        );
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      if (message.trim() && selectedTeamId) {
        handleSendMessage(e);
      }
    }
  };

  // show messages from selected run if available
  const displayMessages = selectedRun ? selectedRun.messages : messages;
  const actualRun = selectedRun || run || null;
  const runStatus = actualRun?.status;
  const canSendMessage = status !== "thinking" && runStatus !== "complete" && runStatus !== "error" && runStatus !== "stopped";

  // Should we show the streaming message?
  const showStreamingMessage = !selectedRun && currentStreamingContent && currentStreamingMessage;

  return (
    <div className="w-full h-screen flex flex-col justify-center min-w-full items-center transition-all duration-300 ease-in-out">
      <div className="flex-1 w-full overflow-hidden relative">
        <ScrollArea ref={containerRef} className="w-full h-full py-12">
          <div className="flex flex-col space-y-5">
            {displayMessages.length === 0 && !showStreamingMessage && <NoMessagesState onNewSession={onNewSession} />}
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
          {getStatusDisplay(status, runStatus)}
          <div className="flex items-center gap-2 text-xs">
            <span>Usage: </span>
            <span>{tokenStats.total}</span>
            <div className="flex items-center gap-2">
              <div className="flex items-center gap-1">
                <ArrowLeft className="h-3 w-3" />
                <span>{tokenStats.input}</span>
              </div>
              <div className="flex items-center gap-1">
                <ArrowRightFromLine className="h-3 w-3" />
                <span>{tokenStats.output}</span>
              </div>
            </div>
          </div>
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

            {(runStatus === "complete" || runStatus === "error" || runStatus === "stopped") && onNewSession && (
              <Button onClick={onNewSession} className="bg-violet-500 hover:bg-violet-600" type="button">
                Start New Chat
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
