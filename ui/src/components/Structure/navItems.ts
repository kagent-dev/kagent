import {
  Bot,
  Boxes,
  Cpu,
  LayoutDashboard,
  MessageSquareText,
  Server,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { paths } from "@/router/routes";

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
 * The application's own navigation. Vendor extensions contribute additional
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
  { key: "agents", label: "Agents", path: paths.agents, icon: Bot, order: 200 },
  
  { key: "models", label: "Models", path: paths.models, icon: Cpu, order: 300 },
  {
    key: "mcpServers",
    label: "MCP Servers",
    path: paths.mcpServers,
    icon: Server,
    order: 400,
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
