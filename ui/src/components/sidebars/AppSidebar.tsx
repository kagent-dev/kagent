"use client";

import {
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarRail,
} from "@/components/ui/sidebar";
import KagentLogo from "@/components/kagent-logo";
import { AppSidebarNav } from "./AppSidebarNav";
import { NamespaceSelector } from "./NamespaceSelector";
import { useNamespace } from "@/lib/namespace-context";

export function AppSidebar() {
  const { namespace, setNamespace } = useNamespace();

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1">
          <KagentLogo className="h-6 w-6 shrink-0" />
          <span className="font-semibold text-sm group-data-[collapsible=icon]:hidden">
            KAgent
          </span>
        </div>
        <NamespaceSelector value={namespace} onValueChange={setNamespace} />
      </SidebarHeader>
      <SidebarContent>
        <AppSidebarNav />
      </SidebarContent>
      <SidebarFooter>
        <div />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
