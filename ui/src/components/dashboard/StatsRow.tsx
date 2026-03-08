import { Bot, GitBranch, Clock, Brain, Wrench, Server, GitFork } from "lucide-react";
import { DashboardCounts } from "@/types";
import { StatCard } from "./StatCard";

interface StatsRowProps {
  counts: DashboardCounts;
}

const STAT_CARDS = [
  { key: "agents" as const, icon: Bot, label: "My Agents" },
  { key: "workflows" as const, icon: GitBranch, label: "Workflows" },
  { key: "cronJobs" as const, icon: Clock, label: "Cron Jobs" },
  { key: "models" as const, icon: Brain, label: "Models" },
  { key: "tools" as const, icon: Wrench, label: "Tools" },
  { key: "mcpServers" as const, icon: Server, label: "MCP Servers" },
  { key: "gitRepos" as const, icon: GitFork, label: "Git Repos" },
];

export function StatsRow({ counts }: StatsRowProps) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-4">
      {STAT_CARDS.map(({ key, icon, label }) => (
        <StatCard key={key} icon={icon} label={label} count={counts[key]} />
      ))}
    </div>
  );
}
