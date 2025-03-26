import type { Team } from "@/types/datamodel";
import { AgentCard } from "./AgentCard";

interface AgentGridProps {
  teams: Team[];
}

export function AgentGrid({ teams }: AgentGridProps) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      {teams.map((team) => (
        <AgentCard key={team.component.label} team={team} />
      ))}
    </div>
  );
}