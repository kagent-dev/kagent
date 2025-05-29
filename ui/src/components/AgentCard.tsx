import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { AgentResponse } from "@/types/datamodel";
import { DeleteButton } from "@/components/DeleteAgentButton";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Pencil } from "lucide-react";
import { Shield } from "lucide-react";
import { Badge } from "@/components/ui/badge";

interface AgentCardProps {
  agentResponse: AgentResponse;
  id: number;
}

export function AgentCard({ id, agentResponse: { agent, model, provider } }: AgentCardProps) {
  const router = useRouter();
  const isA2AEnabled = agent?.spec?.a2aConfig?.auth?.enabled;

  const handleEditClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    router.push(`/agents/new?edit=true&id=${id}`);
  };

  return (
    <Link href={`/agents/${id}/chat`} passHref>
      <Card className={`group transition-colors cursor-pointer hover:border-violet-500`}>
        <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardTitle className="flex items-center gap-2">
            <KagentLogo className="h-5 w-5" />
            {agent.metadata.name}
          </CardTitle>
          <div className="flex items-center space-x-2 invisible group-hover:visible">
            <Button variant="ghost" size="icon" onClick={handleEditClick} aria-label="Edit Agent">
              <Pencil className="h-4 w-4" />
            </Button>
            <DeleteButton teamLabel={String(agent.metadata.name)} />
          </div>
        </CardHeader>
        <CardContent className="flex flex-col justify-between h-32">
          <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden">{agent.spec.description}</p>
          <div className="mt-4 flex items-center text-xs text-muted-foreground">
            <span>
              {provider} ({model})
            </span>
          </div>
          <div className="card-footer flex items-center justify-between">
            {isA2AEnabled && (
              <Badge variant="outline" className="ml-auto flex items-center gap-1">
                <Shield size={14} />
                A2A
              </Badge>
            )}
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
