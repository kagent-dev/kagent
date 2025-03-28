import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import type { AgentResponse } from "@/types/datamodel";
import { DeleteButton } from "@/components/DeleteAgentButton";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";

interface AgentCardProps {
  agentResponse: AgentResponse;
  id: number;
}

export function AgentCard({ id, agentResponse: { agent, model, provider } }: AgentCardProps) {
  return (
    <Link href={`/agents/${id}/chat`}>
      <Card className={`transition-colors cursor-pointer hover:border-violet-500`}>
        <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardTitle className="flex items-center gap-2">
            <KagentLogo className="h-5 w-5" />
            {agent.metadata.name}
          </CardTitle>
          <DeleteButton teamLabel={String(agent.metadata.name)} />
        </CardHeader>
        <CardContent className="flex flex-col justify-between h-32">
          <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden">{agent.spec.description}</p>
          <div className="mt-4 flex items-center text-xs text-muted-foreground">
            <span>
              {provider} ({model})
            </span>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
