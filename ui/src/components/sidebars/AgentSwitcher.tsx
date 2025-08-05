"use client";

import * as React from "react";
import { ChevronsUpDown, Plus } from "lucide-react";

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuShortcut, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem, useSidebar } from "@/components/ui/sidebar";
import type { AgentResponse } from "@/types";
import KagentLogo from "../kagent-logo";
import { useRouter } from "next/navigation";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface AgentSwitcherProps {
  currentAgent: AgentResponse;
  allAgents: AgentResponse[];
}

export function AgentSwitcher({ currentAgent, allAgents }: AgentSwitcherProps) {
  const router = useRouter();
  const { isMobile } = useSidebar();

  const selectedTeam = currentAgent;
  const agentResponses = allAgents;

  if (!selectedTeam) {
    return null;
  }

  const selectedTeamRef = k8sRefUtils.toRef(
    selectedTeam.agent.metadata.namespace || "",
    selectedTeam.agent.metadata.name
  );

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton size="lg" className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground">
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary/5 text-sidebar-primary-foreground">
                <KagentLogo className="w-4 h-4" />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">{selectedTeamRef}</span>
                <span className="truncate text-xs">{selectedTeam.modelProvider} ({selectedTeam.model})</span>
              </div>
              <ChevronsUpDown className="ml-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-[--radix-dropdown-menu-trigger-width] min-w-56 rounded-lg" align="start" side={isMobile ? "bottom" : "right"} sideOffset={4}>
            <DropdownMenuLabel className="text-xs text-muted-foreground">Agents</DropdownMenuLabel>
            {agentResponses.map(({ id, agent}, index) => {
              const agentRef = k8sRefUtils.toRef(agent.metadata.namespace || "", agent.metadata.name)
              return (
                <DropdownMenuItem
                  key={agentRef}
                  onClick={() => {
                    router.push(`/agents/${agentRef}/chat`);
                  }}
                  className="gap-2 p-2"
                >
                  {agentRef}
                  <DropdownMenuShortcut>⌘{index + 1}</DropdownMenuShortcut>
                </DropdownMenuItem>
              );
            })}
            <DropdownMenuSeparator />
            <DropdownMenuItem className="gap-2 p-2" onClick={() => router.push("/agents/new")}>
              <div className="flex size-6 items-center justify-center rounded-md border bg-background">
                <Plus className="size-4" />
              </div>
              <div className="font-medium text-muted-foreground">New agent</div>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
