"use client";
import { AgentGrid } from "@/components/AgentGrid";
import { Plus } from "lucide-react";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { ErrorState } from "./ErrorState";
import { Button } from "./ui/button";
import { LoadingState } from "./LoadingState";
import { useAgents } from "./AgentsProvider";

export default function AgentList() {
  const { agents , loading, error } = useAgents();

  if (error) {
    return <ErrorState message={error} />;
  }

  if (loading) {
    return <LoadingState />;
  }

  return (
    <div className="mt-12 mx-auto max-w-6xl px-6">
      <div className="flex justify-between items-center mb-8">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Agents</h1>
        </div>
      </div>

      {agents?.length === 0 ? (
        <div className="text-center py-12">
          <KagentLogo className="h-16 w-16 mx-auto mb-4" />
          <h3 className="text-lg font-medium  mb-2">No agents yet</h3>
          <p className=" mb-6">Create your first agent to get started</p>
          <Button className="bg-violet-500 hover:bg-violet-600" asChild>
            <Link href={"/agents/new"}>
              <Plus className="h-4 w-4 mr-2" />
              Create New Agent
            </Link>
          </Button>
        </div>
      ) : (
        <AgentGrid agentResponse={agents || []} />
      )}
    </div>
  );
}
