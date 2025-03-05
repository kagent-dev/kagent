"use client";

import { AgentGrid } from "@/components/AgentGrid";
import { Button } from "@/components/ui/button";
import { Team } from "@/types/datamodel";
import { Plus, RefreshCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useAgents } from "@/components/AgentsProvider";
import KagentLogo from "@/components/kagent-logo";

interface AgentListProps {
  teams: Team[];
}

const AgentCardSkeleton = () => {
  return (
    <div className="bg-[#2A2A2A] border border-[#3A3A3A] rounded-lg animate-pulse">
      <div className="p-6 pb-2">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-2">
            <div className="h-5 w-5 rounded-full bg-violet-500/30"></div>
            <div className="h-5 w-32 bg-gray-700 rounded"></div>
          </div>
          <div className="h-8 w-8 bg-gray-700 rounded-md"></div>
        </div>
      </div>
      <div className="px-6 py-4">
        <div className="h-4 w-full bg-gray-700 rounded mb-2"></div>
        <div className="h-4 w-4/5 bg-gray-700 rounded mb-6"></div>
        <div className="mt-4 flex items-center">
          <div className="h-3 w-40 bg-gray-700 rounded"></div>
        </div>
      </div>
    </div>
  );
};

const AgentGridSkeleton = () => {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      {Array(1)
        .fill(0)
        .map((_, index) => (
          <AgentCardSkeleton key={index} />
        ))}
    </div>
  );
};

export default function AgentList({ teams }: AgentListProps) {
  const router = useRouter();
  const { refreshTeams, loading } = useAgents();
  const [isRefreshing, setIsRefreshing] = useState(false);

  const handleRefresh = async () => {
    setIsRefreshing(true);
    await refreshTeams();
    setIsRefreshing(false);
  };

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex justify-between items-center mb-8">
          <div className="flex items-center gap-4">
            <h1 className="text-2xl font-bold text-primary-foreground">Your Agents</h1>
            <Button variant="outline" size="sm" onClick={handleRefresh} disabled={isRefreshing}>
              <RefreshCw className={`h-4 w-4 ${isRefreshing ? "animate-spin" : ""}`} />
            </Button>
          </div>
          <Button onClick={() => router.push("/agents/new")} className="bg-violet-500 hover:bg-violet-600">
            <Plus className="h-4 w-4 mr-2" />
            Create New Agent
          </Button>
        </div>

        {loading ? (
          <AgentGridSkeleton />
        ) : teams?.length === 0 ? (
          <div className="text-center py-12">
            <KagentLogo className="h-16 w-16 text-white/20 mx-auto mb-4" />
            <h3 className="text-lg font-medium text-white mb-2">No agents yet</h3>
            <p className="text-white/50 mb-6">Create your first agent to get started</p>
            <Button onClick={() => router.push("/agents/new")} className="bg-violet-500 hover:bg-violet-600">
              <Plus className="h-4 w-4 mr-2" />
              Create New Agent
            </Button>
          </div>
        ) : (
          <AgentGrid teams={teams || []} />
        )}
      </div>
    </div>
  );
}