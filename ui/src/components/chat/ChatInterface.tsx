"use client";

import type React from "react";
import { useState, useRef, useEffect } from "react";
import { ArrowLeft, ArrowRightFromLine, ArrowBigUp, AlertTriangle, CheckCircle, MessageSquare, StopCircle, X, MessageSquarePlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { Team, Message, RunStatus, Session, Run } from "@/types/datamodel";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import useChatStore from "@/lib/useChatStore";
import { ChatStatus } from "@/lib/ws";
import StreamingMessage from "./StreamingMessage";

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
  selectedAgentTeam?: Team | null;
  selectedSession?: Session | null;
  selectedRun?: Run | null;
  onNewSession?: () => void;
}

export default function ChatInterface({ selectedAgentTeam, selectedRun, onNewSession }: ChatInterfaceProps) {
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
    const msgs = selectedRun ? selectedRun.messages : messages
    if (msgs.length > 0) {
      const newStats = calculateTokenStats(msgs);
      setTokenStats(newStats);
    }
  }, [messages, selectedRun]);

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!message.trim() || !selectedAgentTeam?.id) {
      return;
    }

    const currentMessage = message;
    setMessage("");

    try {
      await sendUserMessage(currentMessage, selectedAgentTeam.id);
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
          <div className="text-white/50 text-xs justify-center items-center flex">
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
                <div className="text-white/50 text-xs justify-center items-center flex">
                  <CheckCircle size={16} className="mr-2 text-green-500" />
                  Task completed
                </div>
              );
            case "error":
            case "timeout":
              return (
                <div className="text-white/50 text-xs justify-center items-center flex">
                  <AlertTriangle size={16} className="mr-2 text-red-500" />
                  {run?.error_message || "An error occurred"}
                </div>
              );
            case "stopped":
              return (
                <div className="text-white/50 text-xs justify-center items-center flex">
                  <StopCircle size={16} className="mr-2 text-orange-500" />
                  Task was stopped
                </div>
              );
            default:
              return (
                <div className="text-white/50 text-xs justify-center items-center flex">
                  <MessageSquare size={16} className="mr-2 text-white" />
                  Ready
                </div>
              );
          }
        }

        return (
          <div className="text-white/50 text-xs justify-center items-center flex">
            <MessageSquare size={16} className="mr-2 text-white" />
            Ready
          </div>
        );
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      if (message.trim() && selectedAgentTeam) {
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
    <div className="w-full h-screen flex flex-col justify-center transition-all duration-300 ease-in-out pt-[1.5rem]">
      <div ref={containerRef} className="flex-1 overflow-y-auto my-8 transition-all duration-300 p-4 -translate-y-[1.5rem]">
        <ScrollArea>
          <div className="flex flex-col space-y-5">
            {displayMessages.length === 0 && !showStreamingMessage && (
              <div className="flex flex-col items-center justify-center px-4 py-12 text-center">
                <MessageSquarePlus className="mb-4 h-8 w-8 text-violet-500" />
                <h3 className="mb-2 text-xl font-medium text-white/90">Ready to start chatting?</h3>
                <p className="text-base text-white/60">
                  Begin a new conversation with <span className="font-medium text-white/90">{selectedAgentTeam?.component.label}</span>
                </p>
                {onNewSession && (
                  <Button onClick={onNewSession} className="mt-4 bg-violet-500 hover:bg-violet-600">
                    Start New Chat
                  </Button>
                )}
              </div>
            )}
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

      <div className="rounded-lg bg-[#2A2A2A] border border-[#3A3A3A] overflow-hidden transition-all duration-300 ease-in-out -translate-y-[1.5rem]">
        <div className="p-4">
          <div className="flex items-center justify-between mb-4">
            {getStatusDisplay(status, runStatus)}
            <div className="flex items-center gap-2 text-xs text-white/50">
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
              className={`min-h-[100px] bg-transparent border-0 p-0 focus-visible:ring-0 resize-none ${
                canSendMessage ? "text-white placeholder:text-white/40" : "text-white/40 placeholder:text-white/40"
              }`}
            />

            <div className="flex items-center justify-end gap-2 mt-4">
              {canSendMessage && (
                <Button type="submit" className={"bg-white hover:bg-white/60 text-black"} disabled={!selectedAgentTeam || !message.trim()}>
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
                <Button onClick={handleCancel} className="bg-white/60 hover:bg-white/90 text-black" type="button">
                  Cancel
                  <X className="h-4 w-4 ml-2" />
                </Button>
              )}
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}
