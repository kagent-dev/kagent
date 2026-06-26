"use client";

// ACP harness chat — drives the standard kagent chat experience over the
// Agent Client Protocol for substrate AgentHarness actors, through the
// controller's same-origin WebSocket proxy (/api/agentharnesses/{ns}/{name}/acp).
//
// This component is the presentational shell: useAcpHarnessChat owns the ACP
// transport and exposes the render state (conn / messages / streamingContent)
// plus connect / sendMessage / cancel. Streaming output is rendered with the
// same A2A Message shapes the rest of the chat UI uses (ChatMessage /
// StreamingMessage).

import type React from "react";
import { useEffect, useRef, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import ChatMessage from "@/components/chat/ChatMessage";
import StreamingMessage from "./StreamingMessage";
import AcpChatEmptyState from "./AcpChatEmptyState";
import AcpChatComposer from "./AcpChatComposer";
import { connToChatStatus } from "@/lib/acp";
import { useAcpHarnessChat } from "@/hooks/useAcpHarnessChat";
import type { AcpSessionInfo } from "@/types/acp";

interface AcpHarnessChatProps {
  /** Same-origin WebSocket path, e.g. /api/agentharnesses/kagent/my-claw/acp */
  acpPath: string;
  namespace: string;
  agentName: string;
  /** The kagent/DB session id this chat maps to. For harness chats this also is
   * the ACP session id, so a reopened chat resumes the right conversation. */
  sessionId?: string;
  /** Callback when ACP sessions are updated from session/list. */
  onSessionsUpdate?: (sessions: AcpSessionInfo[]) => void;
  /** The session ID to load on mount (from sidebar click). */
  initialLoadSessionId?: string;
  /** Connect on mount and resume the actor's prior transcript (existing chats).
   * New chats pass false so they stay idle until the first message. */
  autoConnect?: boolean;
}

export default function AcpHarnessChat({
  acpPath,
  namespace,
  agentName,
  sessionId,
  onSessionsUpdate,
  initialLoadSessionId,
  autoConnect,
}: AcpHarnessChatProps) {
  const { conn, messages, streamingContent, sendMessage, cancel } = useAcpHarnessChat({
    acpPath,
    namespace,
    agentName,
    sessionId,
    onSessionsUpdate,
    initialLoadSessionId,
    autoConnect,
  });

  const [currentInputMessage, setCurrentInputMessage] = useState("");
  const bottomRef = useRef<HTMLDivElement | null>(null);

  const chatStatus = connToChatStatus(conn);
  const agentContext = { namespace, agentName };
  const connectingHint =
    conn === "connecting" || conn === "initializing" || conn === "creating-session" || conn === "loading-session";

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingContent, conn]);

  const handleSendMessage = (e: React.FormEvent) => {
    e.preventDefault();
    const text = currentInputMessage.trim();
    if (!text) return;
    setCurrentInputMessage("");
    sendMessage(text);
  };

  return (
    <div className="w-full h-screen flex flex-col min-w-full items-center transition-all duration-300 ease-in-out">
      <div className="flex-1 min-h-0 w-full overflow-hidden relative">
        <ScrollArea className="w-full h-full py-12">
          <div className="flex flex-col space-y-5 px-4">
            {messages.length === 0 && !streamingContent ? (
              <AcpChatEmptyState connecting={connectingHint} disconnected={conn === "disconnected"} />
            ) : (
              <>
                {messages.map((message, index) => (
                  <ChatMessage
                    key={message.messageId ?? `acp-${index}`}
                    message={message}
                    allMessages={messages}
                    agentContext={agentContext}
                  />
                ))}
                {streamingContent && <StreamingMessage content={streamingContent} />}
              </>
            )}
            <div ref={bottomRef} />
          </div>
        </ScrollArea>
      </div>

      <AcpChatComposer
        value={currentInputMessage}
        onChange={setCurrentInputMessage}
        onSubmit={handleSendMessage}
        chatStatus={chatStatus}
        showCancel={conn === "running"}
        onCancel={cancel}
      />
    </div>
  );
}
