"use client";

import { useRouter } from "next/navigation";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import type { Team } from "@/types/datamodel";
import { DeleteButton } from "@/components/DeleteAgentButton";
import KagentLogo from "@/components/kagent-logo";
import { getUsersAgentFromTeam } from "@/lib/agents";

interface AgentCardProps {
  team: Team;
}

export function AgentCard({ team }: AgentCardProps) {
  const router = useRouter();
  const agent = getUsersAgentFromTeam(team);

  const handleCardClick = () => {
      router.push(`/agents/${team.id}/chat`);
  };

  return (
    <Card 
      className={`bg-[#2A2A2A] border-[#3A3A3A] transition-colors cursor-pointer hover:border-violet-500`}
      onClick={handleCardClick}
    >
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="flex items-center gap-2 text-white">
          <KagentLogo className="h-5 w-5" />
          {agent.label}
        </CardTitle>
        <DeleteButton 
          teamId={String(team.id)} 
        />
      </CardHeader>
      <CardContent>
        <p className="text-white/70 text-sm">{agent.description}</p>
        <div className="mt-4 flex items-center text-xs text-white/50">
          <span>Model: {agent.config.model_client.config.model}</span>
        </div>
      </CardContent>
    </Card>
  );
}