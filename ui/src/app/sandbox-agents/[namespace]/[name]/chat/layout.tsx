import ChatLayoutServer from "@/components/chat/ChatLayoutServer";
import { ReactNode } from "react";
import type { Metadata } from "next";

export const metadata: Metadata = {
  referrer: "no-referrer",
};

// SandboxAgent chats live under /sandbox-agents so a namespace/name shared with
// a plain Agent still resolves to the sandbox agent (the kind travels in the
// path, not a droppable query param).
export default function SandboxChatLayout(props: {
  children: ReactNode;
  params: Promise<{ name: string; namespace: string }>;
}) {
  return <ChatLayoutServer {...props} agentKind="SandboxAgent" />;
}
