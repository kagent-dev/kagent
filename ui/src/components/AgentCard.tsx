import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import type { Team } from "@/types/datamodel";
import { DeleteButton } from "@/components/DeleteAgentButton";
import KagentLogo from "@/components/kagent-logo";
import { getUsersAgentFromTeam } from "@/lib/agents";
import Link from "next/link";

interface AgentCardProps {
  team: Team;
}

export function AgentCard({ team }: AgentCardProps) {
  const agent = getUsersAgentFromTeam(team);


  return (
    <Link href={`/agents/${team.id}/chat`}>
      <Card className={`transition-colors cursor-pointer hover:border-violet-500`}>
        <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardTitle className="flex items-center gap-2">
            <KagentLogo className="h-5 w-5" />
            {agent.label || agent.config.name}
          </CardTitle>
          <DeleteButton teamId={String(team.id)} />
        </CardHeader>
        <CardContent className="flex flex-col justify-between h-32">
          <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden">{agent.description}</p>
          <div className="mt-4 flex items-center text-xs text-muted-foreground">
            <span>Model: {agent.config.model_client.config.model}</span>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}