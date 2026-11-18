import { Bot, Cable, Cpu, LayoutTemplate, Server, type LucideIcon } from "lucide-react";

/** One icon per resource kind, shared by the nav, resource links and details pages. */
export const resourceIcons = {
  agent: Bot,
  model: Cpu,
  mcpServer: Server,
  template: LayoutTemplate,
  harness: Cable,
} satisfies Record<string, LucideIcon>;
