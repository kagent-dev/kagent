import {
  CalendarClock,
  Boxes,
  LayoutDashboard,
  MessageSquareText,
  ShieldCheck,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { resourceIcons } from "@/components/common/resourceIcons";
import { paths } from "@/router/routes";

/** Where every rail icon's left edge sits, from the row's inset: the same expanded and collapsed. */
export const NAV_INSET = 18;

export interface NavItem {
  /** Stable identifier; also the antd Menu key. */
  key: string;
  label: string;
  path: string;
  icon: LucideIcon;
  /** Lower sorts first. Core items use multiples of 100 to leave gaps. */
  order: number;
}

/**
 * The application's own navigation. App extensions contribute additional
 * items rather than editing this list — see the navigation extension point.
 */
export const coreNavItems: NavItem[] = [
  {
    key: "dashboard",
    label: "Dashboard",
    path: paths.dashboard,
    icon: LayoutDashboard,
    order: 100,
  },
  { key: "agents", label: "Agents", path: paths.agents, icon: resourceIcons.agent, order: 200 },
  { key: "scheduledRuns", label: "Schedules", path: paths.scheduledRuns, icon: CalendarClock, order: 275 },
  
  { key: "models", label: "Models", path: paths.models, icon: resourceIcons.model, order: 300 },
  {
    key: "mcpServers",
    label: "MCP Servers",
    path: paths.mcpServers,
    icon: resourceIcons.mcpServer,
    order: 400,
  },
  {
    key: "policies",
    label: "Policies",
    path: paths.policies,
    icon: ShieldCheck,
    order: 450,
  },
  {
    key: "prompts",
    label: "Prompts",
    path: paths.prompts,
    icon: MessageSquareText,
    order: 500,
  },
  {
    key: "substrate",
    label: "Substrate",
    path: paths.substrate,
    icon: Boxes,
    order: 600,
  },
];
