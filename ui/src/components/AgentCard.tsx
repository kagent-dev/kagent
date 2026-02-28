"use client";

import { useState } from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { AgentResponse } from "@/types";
import { DeleteButton } from "@/components/DeleteAgentButton";
import { MemoriesDialog } from "@/components/MemoriesDialog";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Brain, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { cn } from "@/lib/utils";

interface AgentCardProps {
  agentResponse: AgentResponse;
}

export function AgentCard({ agentResponse: { agent, model, modelProvider, deploymentReady, accepted } }: AgentCardProps) {
  const router = useRouter();
  const [memoriesOpen, setMemoriesOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

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

  const getStatusInfo = () => {
    if (!accepted) {
      return {
        message: "Agent not Accepted",
        className:"bg-red-500/10 text-red-600 dark:text-red-500"
      };
    }
    if (!deploymentReady) {
      return {
        message: "Agent not Ready",
        className:"bg-yellow-400/30 text-yellow-800 dark:bg-yellow-500/40 dark:text-yellow-200"
      };
    }
    return null;
  };

  const statusInfo = getStatusInfo();

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
        <div className="relative z-30 opacity-0 group-hover:opacity-100 transition-opacity">
          <DropdownMenu>
            <DropdownMenuTrigger asChild onClick={(e) => { e.preventDefault(); e.stopPropagation(); }}>
              <Button
                variant="ghost"
                size="icon"
                aria-label="Agent options"
                className="bg-background/80 hover:bg-background shadow-sm"
              >
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
              <DropdownMenuItem
                onClick={handleEditClick}
                className="cursor-pointer"
              >
                <Pencil className="mr-2 h-4 w-4" />
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setMemoriesOpen(true);
                }}
                className="cursor-pointer"
              >
                <Brain className="mr-2 h-4 w-4" />
                View Memories
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  setDeleteOpen(true);
                }}
                className="cursor-pointer text-red-500 focus:text-red-500"
              >
                <Trash2 className="mr-2 h-4 w-4" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col justify-between h-32 relative z-10">
        <p className="text-sm text-muted-foreground line-clamp-3 overflow-hidden">
          {agent.spec.description}
        </p>
        <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
          {isBYO ? (
            <span title={byoImage} className="truncate">Image: {byoImage}</span>
          ) : (
            <span className="truncate">{modelProvider} ({model})</span>
          )}
          {!isBYO && agent.spec?.declarative?.a2aConfig?.skills && agent.spec.declarative.a2aConfig.skills.length > 0 && (
            <span className="inline-flex items-center rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400 shrink-0">
              A2A
            </span>
          )}
        </div>
      </CardContent>
      {statusInfo && (
        <div className={cn(
          "absolute bottom-0 left-0 right-0 z-20 py-1.5 px-4 text-right text-xs font-medium rounded-b-xl",
          statusInfo.className
        )}>
          {statusInfo.message}
        </div>
      )}

    </Card>
  );

  return (
    <>
      {isReady ? (
        <Link href={`/agents/${agent.metadata.namespace}/${agent.metadata.name}/chat`} passHref>
          {cardContent}
        </Link>
      ) : (
        cardContent
      )}

      <DeleteButton
        agentName={agent.metadata.name}
        namespace={agent.metadata.namespace || ''}
        externalOpen={deleteOpen}
        onExternalOpenChange={setDeleteOpen}
      />

      <MemoriesDialog
        agentName={agent.metadata.name || ''}
        namespace={agent.metadata.namespace || ''}
        open={memoriesOpen}
        onOpenChange={setMemoriesOpen}
      />
    </>
  );
}
