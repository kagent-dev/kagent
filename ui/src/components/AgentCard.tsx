import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { AgentResponse } from "@/types";
import { DeleteButton } from "@/components/DeleteAgentButton";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Pencil, AlertCircle, Clock } from "lucide-react";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { cn } from "@/lib/utils";

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
  const isReady = deploymentReady && accepted;

  const handleEditClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    router.push(`/agents/new?edit=true&name=${agent.metadata.name}&namespace=${agent.metadata.namespace}`);
  };

  const cardContent = (
    <Card className={cn(
      "group relative transition-all duration-200 overflow-hidden min-h-[200px]",
      isReady
        ? 'cursor-pointer hover:border-primary hover:shadow-md'
        : 'cursor-default'
    )}>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2 relative z-30">
        <CardTitle className="flex items-center gap-2 flex-1 min-w-0">
          <KagentLogo className="h-5 w-5 flex-shrink-0" />
          <span className="truncate">{agentRef}</span>
        </CardTitle>
        <div className="flex items-center space-x-2 relative z-30 opacity-0 group-hover:opacity-100 transition-opacity">
          <Button
            variant="ghost"
            size="icon"
            onClick={handleEditClick}
            aria-label="Edit Agent"
            className="bg-background/80 hover:bg-background shadow-sm"
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <DeleteButton
            agentName={agent.metadata.name}
            namespace={agent.metadata.namespace || ''}
          />
        </div>
      </CardHeader>
      <CardContent className="flex flex-col justify-between h-32 relative z-10">
        <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden">
          {agent.spec.description}
        </p>
        <div className="mt-4 flex items-center text-xs text-muted-foreground">
          {isBYO ? (
            <span title={byoImage} className="truncate">Image: {byoImage}</span>
          ) : (
            <span className="truncate">{modelProvider} ({model})</span>
          )}
        </div>
      </CardContent>

      {!isReady && (
        <div className={cn(
          "absolute inset-0 rounded-xl flex flex-col items-center justify-center z-20 backdrop-blur-[2px]",
          !accepted 
            ? "bg-destructive/90" 
            : "bg-secondary/90"
        )}>
          <div className="text-center px-6 py-8 max-w-[80%]">
            <div className="flex justify-center mb-4">
              {!accepted ? (
                <AlertCircle className="h-12 w-12 text-destructive-foreground drop-shadow-lg" />
              ) : (
                <Clock className="h-12 w-12 text-secondary-foreground drop-shadow-lg" />
              )}
            </div>
            <h3 className={cn(
              "font-bold text-2xl mb-3 drop-shadow-lg",
              !accepted ? "text-destructive-foreground" : "text-secondary-foreground"
            )}>
              {!accepted ? "Agent not Accepted" : "Agent not Ready"}
            </h3>
            <p className={cn(
              "text-base drop-shadow",
              !accepted ? "text-destructive-foreground/95" : "text-secondary-foreground/90"
            )}>
              {!accepted 
                ? "Configuration needs review" 
                : "Still deploying..."}
            </p>
          </div>
        </div>
      )}
    </Card>
  );

  return isReady ? (
    <Link href={`/agents/${agent.metadata.namespace}/${agent.metadata.name}/chat`} passHref>
      {cardContent}
    </Link>
  ) : (
    cardContent
  );
}
