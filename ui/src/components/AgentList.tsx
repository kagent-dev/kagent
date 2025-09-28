"use client";
import { AgentGrid } from "@/components/AgentGrid";
import { Plus } from "lucide-react";
import KagentLogo from "@/components/kagent-logo";
import Link from "next/link";
import { ErrorState } from "./ErrorState";
import { Button } from "./ui/button";
import { LoadingState } from "./LoadingState";
import { useAgents } from "./AgentsProvider";
import { useEffect, useMemo, useState } from "react";

export default function AgentList() {
  const { agents , loading, error } = useAgents();
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});

  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch('/api/admin/agents-settings');
        if (res.ok) {
          const data = await res.json();
          setEnabledMap(data.enabled || {});
        }
      } catch {
        // ignore and default to all enabled
      }
    };
    load();
  }, []);

  const visibleAgents = useMemo(() => {
    return (agents || []).filter(a => {
      const ns = a.agent.metadata.namespace || '';
      const name = a.agent.metadata.name;
      const ref = `${ns}/${name}`;
      const flag = enabledMap.hasOwnProperty(ref) ? enabledMap[ref] : true;
      return flag;
    });
  }, [agents, enabledMap]);

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

      {visibleAgents?.length === 0 ? (
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
        <AgentGrid agentResponse={visibleAgents || []} />
      )}
    </div>
  );
}
