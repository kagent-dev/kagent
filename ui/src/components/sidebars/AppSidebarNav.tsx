"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Activity,
  Bot,
  GitBranch,
  Clock,
  Kanban,
  Brain,
  Wrench,
  Server,
  GitFork,
  Building2,
  Network,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from "@/components/ui/sidebar";

interface NavItem {
  label: string;
  href: string;
  icon: LucideIcon;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

export const NAV_SECTIONS: NavSection[] = [
  {
    label: "OVERVIEW",
    items: [
      { label: "Dashboard", href: "/", icon: LayoutDashboard },
      { label: "Live Feed", href: "/feed", icon: Activity },
    ],
  },
  {
    label: "AGENTS",
    items: [
      { label: "My Agents", href: "/agents", icon: Bot },
      { label: "Workflows", href: "/workflows", icon: GitBranch },
      { label: "Cron Jobs", href: "/cronjobs", icon: Clock },
      { label: "Kanban", href: "/kanban", icon: Kanban },
    ],
  },
  {
    label: "RESOURCES",
    items: [
      { label: "Models", href: "/models", icon: Brain },
      { label: "Tools", href: "/tools", icon: Wrench },
      { label: "MCP Servers", href: "/servers", icon: Server },
      { label: "GIT Repos", href: "/git", icon: GitFork },
    ],
  },
  {
    label: "ADMIN",
    items: [
      { label: "Organization", href: "/admin/org", icon: Building2 },
      { label: "Gateways", href: "/admin/gateways", icon: Network },
    ],
  },
];

export function AppSidebarNav() {
  const pathname = usePathname();

  return (
    <>
      {NAV_SECTIONS.map((section) => {
        const sectionId = `nav-section-${section.label.toLowerCase()}`;
        return (
          <SidebarGroup key={section.label} role="group" aria-labelledby={sectionId}>
            <SidebarGroupLabel id={sectionId}>{section.label}</SidebarGroupLabel>
            <SidebarMenu>
              {section.items.map((item) => {
                const isActive = pathname === item.href;
                return (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      asChild
                      isActive={isActive}
                      aria-current={isActive ? "page" : undefined}
                    >
                      <Link href={item.href}>
                        <item.icon />
                        <span>{item.label}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroup>
        );
      })}
    </>
  );
}
