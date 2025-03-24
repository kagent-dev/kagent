import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sidebar, SidebarContent, SidebarHeader, SidebarRail } from "../ui/sidebar";
import { AgentSwitcher } from "./AgentSwitcher";
import GroupedSessions from "./GroupedSessions";

interface SessionsSidebarProps {
  agentId: number;
}

export default function SessionsSidebar({ agentId }: SessionsSidebarProps) {
  return (
    <Sidebar side="left" collapsible="offcanvas">
      <SidebarHeader>
        <AgentSwitcher agentId={agentId} />
      </SidebarHeader>
      <SidebarContent>
        <ScrollArea className="flex-1 my-4">
          <GroupedSessions agentId={agentId} />
        </ScrollArea>
      </SidebarContent>
      <SidebarRail />
    </Sidebar>
  );
}
