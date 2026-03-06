"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Activity,
  Bot,
  GitBranch,
  Clock,
  Brain,
  Wrench,
  Server,
  GitFork,
  Building2,
  Network,
  Puzzle,
  Loader2,
  AlertCircle,
  RefreshCw,
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

interface NavItem {
  label: string;
  href: string;
  icon: LucideIcon;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

interface PluginNav {
  name: string;
  pathPrefix: string;
  displayName: string;
  icon: string;
  section: string;
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
  const [plugins, setPlugins] = useState<PluginNav[]>([]);
  const [badges, setBadges] = useState<Record<string, PluginBadge>>({});
  const [pluginsLoading, setPluginsLoading] = useState(true);
  const [pluginsError, setPluginsError] = useState<string | null>(null);
  const [fetchKey, setFetchKey] = useState(0);

  // Fetch plugins on mount (and on retry)
  useEffect(() => {
    setPluginsLoading(true);
    setPluginsError(null);
    fetch("/api/plugins")
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((res) => {
        setPlugins(res.data ?? []);
        setPluginsLoading(false);
      })
      .catch((err) => {
        setPluginsError(err.message || "Failed to load plugins");
        setPluginsLoading(false);
      });
  }, [fetchKey]);

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
        href: `/plugins/${p.pathPrefix}`,
        icon: getIconByName(p.icon),
        badge: badges[p.pathPrefix],
      }));
    return {
      ...section,
      items: [
        ...section.items.map((i) => ({ ...i, badge: undefined as PluginBadge | undefined })),
        ...pluginItems,
      ],
    };
  });

  // Add PLUGINS section for plugins that specify a section not in NAV_SECTIONS
  const pluginsSection = plugins.filter((p) => !knownSections.includes(p.section));
  if (pluginsSection.length > 0) {
    sections.push({
      label: "PLUGINS",
      items: pluginsSection.map((p) => ({
        label: p.displayName,
        href: `/plugins/${p.pathPrefix}`,
        icon: getIconByName(p.icon),
        badge: badges[p.pathPrefix],
      })),
    });
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
      {pluginsLoading && (
        <div data-testid="plugins-loading" className="flex items-center gap-2 px-4 py-2 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          <span>Loading plugins…</span>
        </div>
      )}
      {pluginsError && (
        <div data-testid="plugins-error" className="flex items-center gap-2 px-4 py-2 text-xs text-destructive">
          <AlertCircle className="h-3 w-3" />
          <span>Plugins failed</span>
          <button
            data-testid="plugins-retry"
            onClick={() => setFetchKey((k) => k + 1)}
            className="ml-auto inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs hover:bg-muted"
            aria-label="Retry loading plugins"
          >
            <RefreshCw className="h-3 w-3" />
            Retry
          </button>
        </div>
      )}
    </>
  );
}
