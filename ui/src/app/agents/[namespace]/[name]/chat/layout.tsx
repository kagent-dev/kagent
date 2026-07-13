import ChatLayoutServer from "@/components/chat/ChatLayoutServer";
import { ReactNode } from "react";
import type { Metadata } from "next";

export const metadata: Metadata = {
  referrer: "no-referrer",
};

export default function ChatLayout(props: {
  children: ReactNode;
  params: Promise<{ name: string; namespace: string }>;
}) {
  return <ChatLayoutServer {...props} />;
}
