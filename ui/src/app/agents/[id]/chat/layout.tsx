"use client";
import { ReactNode, use } from "react";
import SessionsSidebar from "@/components/sidebars/SessionsSidebar";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { LoadingState } from "@/components/LoadingState";
import { useChatData } from "@/lib/useChatData";

export default function ChatLayout({ params, children }: { params: Promise<{ id: string }>; children: ReactNode }) {
  const { id } = use(params);

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const [chatData, _] = useChatData({
    agentId: id,
  });

  if (chatData.isLoading || !chatData.agent) {
    return <LoadingState />;
  }

  return (
    <SidebarProvider>
      <SessionsSidebar agentId={id} />
      <main className="w-full max-w-6xl mx-auto">{children}</main>
      <AgentDetailsSidebar selectedTeam={chatData.agent} />
    </SidebarProvider>
  );
}
