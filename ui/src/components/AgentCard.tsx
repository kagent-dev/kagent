import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { AgentResponse } from "@/types";
import { DeleteButton } from "@/components/DeleteAgentButton";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Pencil } from "lucide-react";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface AgentCardProps {
  agentResponse: AgentResponse;
  id: number;
}

export function AgentCard({ id, agentResponse: { agent, model, modelProvider, deploymentReady, accepted } }: AgentCardProps) {
  const router = useRouter();
  const agentRef = k8sRefUtils.toRef(
    agent.metadata.namespace || '',
    agent.metadata.name || '');
  const isBYO = agent.spec?.type === "BYO";
  const byoImage = isBYO ? agent.spec?.byo?.deployment?.image : undefined;

  const handleEditClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    router.push(`/agents/new?edit=true&name=${agent.metadata.name}&namespace=${agent.metadata.namespace}`);
  };

  const cardContent = (
    <Card className={`group transition-colors ${
      deploymentReady && accepted
        ? 'cursor-pointer hover:border-violet-500' 
        : 'cursor-not-allowed opacity-60 border-gray-300'
    }`}>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="flex items-center gap-2">
          <KagentLogo className="h-5 w-5" />
          {agentRef}
          {!accepted && (
            <span className="text-xs bg-red-100 text-red-800 px-2 py-1 rounded-full">
              Not Accepted
            </span>
          )}
          {!deploymentReady && (
            <span className="text-xs bg-yellow-100 text-yellow-800 px-2 py-1 rounded-full">
              Not Ready
            </span>
          )}
        </CardTitle>
        <div className={`flex items-center space-x-2 ${deploymentReady && accepted ? 'invisible group-hover:visible' : 'invisible'}`}>
          <Button 
            variant="ghost" 
            size="icon" 
            onClick={handleEditClick} 
            aria-label="Edit Agent"
            disabled={!deploymentReady || !accepted}
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <DeleteButton 
            agentName={agent.metadata.name} 
            namespace={agent.metadata.namespace || ''} 
            disabled={!deploymentReady || !accepted}
          />
        </div>
      </CardHeader>
      <CardContent className="flex flex-col justify-between h-32">
        <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden">{agent.spec.description}</p>
        <div className="mt-4 flex items-center text-xs text-muted-foreground">
          {isBYO ? (
            <span title={byoImage}>Image: {byoImage}</span>
          ) : (
            <span>{modelProvider} ({model})</span>
          )}
        </div>
      </CardContent>
    </Card>
  );

  return deploymentReady && accepted ? (
    <Link href={`/agents/${agent.metadata.namespace}/${agent.metadata.name}/chat`} passHref>
      {cardContent}
    </Link>
  ) : (
    cardContent
  );
}
