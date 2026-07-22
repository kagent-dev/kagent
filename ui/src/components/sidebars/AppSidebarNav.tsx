"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Bot,
  Brain,
  Wrench,
  Server,
  Puzzle,
  ScrollText,
  Layers,
} from "lucide-react";
import * as LucideIcons from "lucide-react";
import type { LucideIcon } from "lucide-react";
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuBadge,
} from "@/components/ui/sidebar";
import { useSidebarStatus } from "@/lib/sidebar-status-context";
import { useSubstrateEnabled } from "@/contexts/SubstrateFeaturesContext";

interface NavItem {
  label: string;
  href: string;
  icon: LucideIcon;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

export interface PluginNav {
  name: string;
  namespace: string;
  pathPrefix: string;
  displayName: string;
  icon: string;
  section: string;
  defaultPath?: string;
}

interface PluginBadge {
  count?: number;
  label?: string;
}

interface NavItemWithBadge extends NavItem {
  badge?: PluginBadge;
}

function getIconByName(name: string): LucideIcon {
  const pascalCase = name
    .split("-")
    .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
    .join("");
  return (LucideIcons as unknown as Record<string, LucideIcon>)[pascalCase] ?? Puzzle;
}

// Route to the plugin frame, honoring the plugin's configured defaultPath so it
// opens at its intended start route (e.g. "/namespaces/kagent") instead of "/".
function pluginHref(pathPrefix: string, defaultPath?: string): string {
  const base = `/plugins/${pathPrefix}`;
  const sub = defaultPath?.replace(/^\/+/, "");
  return sub ? `${base}/${sub}` : base;
}

export const NAV_SECTIONS: NavSection[] = [
  {
    label: "AGENTS",
    items: [{ label: "My Agents", href: "/agents", icon: Bot }],
  },
  {
    label: "WORKFLOWS",
    items: [],
  },
  {
    label: "KNOWLEDGE",
    items: [],
  },
  {
    label: "EVALUATIONS",
    items: [],
  },
  {
    label: "RESOURCES",
    items: [
      { label: "Models", href: "/models", icon: Brain },
      { label: "Tools", href: "/tools", icon: Wrench },
      { label: "MCP Servers", href: "/servers", icon: Server },
      { label: "Prompt Library", href: "/prompts", icon: ScrollText },
      { label: "Plugins Catalog", href: "/plugins", icon: Puzzle },
    ],
  },
];

export function AppSidebarNav() {
  const pathname = usePathname();
  const { plugins } = useSidebarStatus();
  const substrateEnabled = useSubstrateEnabled();
  const [badges, setBadges] = useState<Record<string, PluginBadge>>({});

  // Listen for badge updates from plugin iframes
  useEffect(() => {
    const handler = (e: Event) => {
      const { plugin, count, label } = (e as CustomEvent).detail;
      setBadges((prev) => ({ ...prev, [plugin]: { count, label } }));
    };
    window.addEventListener("kagent:plugin-badge", handler);
    return () => window.removeEventListener("kagent:plugin-badge", handler);
  }, []);

  // Merge plugins into sections
  const knownSections = NAV_SECTIONS.map((s) => s.label);
  const sections: { label: string; items: NavItemWithBadge[] }[] = NAV_SECTIONS.map((section) => {
    const pluginItems: NavItemWithBadge[] = plugins
      .filter((p) => p.section === section.label)
      .map((p) => ({
        label: p.displayName,
        href: pluginHref(p.pathPrefix, p.defaultPath),
        icon: getIconByName(p.icon),
        badge: badges[p.pathPrefix],
      }));
    const extraItems: NavItemWithBadge[] = [];
    if (section.label === "RESOURCES" && substrateEnabled) {
      extraItems.push({ label: "Substrate", href: "/substrate", icon: Layers });
    }
    return {
      ...section,
      items: [
        ...section.items.map((i) => ({ ...i, badge: undefined as PluginBadge | undefined })),
        ...pluginItems,
        ...extraItems,
      ],
    };
  });

  // Plugins whose declared section isn't a built-in NAV section get grouped under
  // their own section label (e.g. "PLUGINS", "ADMIN") rather than a hardcoded one.
  const extraSections = new Map<string, NavItemWithBadge[]>();
  for (const p of plugins) {
    if (knownSections.includes(p.section)) continue;
    const label = p.section || "PLUGINS";
    const items = extraSections.get(label) ?? [];
    items.push({
      label: p.displayName,
      href: pluginHref(p.pathPrefix, p.defaultPath),
      icon: getIconByName(p.icon),
      badge: badges[p.pathPrefix],
    });
    extraSections.set(label, items);
  }
  for (const [label, items] of extraSections) {
    sections.push({ label, items });
  }

  return (
    <>
      {sections.map((section) => {
        if (section.items.length === 0) return null;
        const sectionId = `nav-section-${section.label.toLowerCase()}`;
        return (
          <SidebarGroup key={section.label} role="group" aria-labelledby={sectionId}>
            <SidebarGroupLabel id={sectionId}>{section.label}</SidebarGroupLabel>
            <SidebarMenu>
              {section.items.map((item) => {
                const isActive = pathname === item.href || pathname.startsWith(item.href + "/");
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
                    {item.badge?.count != null && (
                      <SidebarMenuBadge>{item.badge.count}</SidebarMenuBadge>
                    )}
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
