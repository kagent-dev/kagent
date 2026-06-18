"use client";

import { useEffect, useState } from "react";
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
import { StatusIndicator } from "./StatusIndicator";
import { SidebarCollapseButton } from "./SidebarCollapseButton";
import { ThemeToggle } from "@/components/ThemeToggle";
import { UserMenu } from "@/components/UserMenu";
import { useNamespace } from "@/lib/namespace-context";
import { SidebarStatusProvider } from "@/lib/sidebar-status-context";

export function AppSidebar() {
  const { namespace, setNamespace } = useNamespace();
  const [releaseTag, setReleaseTag] = useState("");

  useEffect(() => {
    fetch("/kagent-release", { cache: "no-store" })
      .then((response) => (response.ok ? response.json() : null))
      .then((data) => setReleaseTag(typeof data?.tag === "string" ? data.tag : ""))
      .catch(() => setReleaseTag(""));
  }, []);

  return (
    <SidebarStatusProvider>
      <Sidebar collapsible="icon" aria-label="Main navigation">
        <SidebarHeader>
          <div className="flex items-center gap-2 px-2 py-1">
            <div className="flex min-w-0 flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <KagentLogo className="h-6 w-6 shrink-0" />
                <span className="text-sm font-semibold group-data-[collapsible=icon]:hidden">
                  KAgent
                </span>
              </div>
              {releaseTag && (
                <span className="ml-8 text-[9px] font-medium leading-none text-muted-foreground/80 group-data-[collapsible=icon]:hidden">
                  {releaseTag}
                </span>
              )}
            </div>
            <div className="ml-auto group-data-[collapsible=icon]:hidden">
              <ThemeToggle />
            </div>
          </div>
          <NamespaceSelector value={namespace} onValueChange={setNamespace} />
        </SidebarHeader>
        <SidebarContent>
          <AppSidebarNav />
        </SidebarContent>
        <SidebarFooter>
          <StatusIndicator />
          <UserMenu variant="sidebar" />
          <SidebarCollapseButton />
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
    </SidebarStatusProvider>
  );
}
