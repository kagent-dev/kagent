import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
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
    agent.metadata.name || '');
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
        ? 'cursor-pointer hover:border-violet-500 hover:shadow-md' 
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
            className="bg-white/80 hover:bg-white dark:bg-gray-800/80 dark:hover:bg-gray-800 shadow-sm"
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <DeleteButton 
            agentName={agent.metadata.name} 
            namespace={agent.metadata.namespace || ''} 
            className="bg-white/80 hover:bg-white dark:bg-gray-800/80 dark:hover:bg-gray-800 shadow-sm"
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
          "absolute bottom-0 left-0 right-0 z-20 py-1.5 px-4 text-right text-xs font-medium rounded-b-xl",
          !accepted 
            ? "bg-red-100 text-red-800 dark:bg-red-900/90 dark:text-red-100" 
            : "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/90 dark:text-yellow-100"
        )}>
          {!accepted ? "Agent not Accepted" : "Agent not Ready"}
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
