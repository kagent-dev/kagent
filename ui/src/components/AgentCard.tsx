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
}

export function AgentCard({ agentResponse: { agent, model, modelProvider, deploymentReady, accepted } }: AgentCardProps) {
  const router = useRouter();
  const agentRef = k8sRefUtils.toRef(
    agent.metadata.namespace || '',
    agent.metadata.name || ''
  );
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
        : 'border-gray-300'
    }`}>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <CardTitle className="flex items-center gap-2 flex-1 min-w-0">
          <KagentLogo className="h-5 w-5 flex-shrink-0" />
          <span className="truncate">{agentRef}</span>
        </CardTitle>
        <div className="flex items-center space-x-2 flex-shrink-0 ml-2">
          <Button
            variant="ghost"
            size="icon"
            onClick={handleEditClick}
            aria-label="Edit Agent"
            className="h-8 w-8"
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <DeleteButton
            agentName={agent.metadata.name}
            namespace={agent.metadata.namespace || ''}
          />
        </div>
      </CardHeader>
      <CardContent className="flex flex-col justify-between h-32 pt-2 relative">
        <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden flex-1">
          {agent.spec.description}
        </p>
        <div className="mt-4 flex items-center text-xs text-muted-foreground">
          {isBYO ? (
            <span title={byoImage} className="truncate">
              Image: {byoImage}
            </span>
          ) : (
            <span className="truncate">
              {modelProvider} ({model})
            </span>
          )}
          
           {/* this handles the ribbon part to  edit it change the py to change height and bg-yellow-400/30 to change transparency levels*/}
        </div>
        {!deploymentReady && (
          <div className="absolute bottom-0 left-0 right-0 bg-yellow-400/30 text-yellow-900 text-xs px-3 py-1 rounded-b-lg flex justify-end items-center">
            <span className="font-bold whitespace-nowrap">Agent not Ready</span>
          </div>
        )}
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
