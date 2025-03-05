"use client";

import AgentList from "@/components/AgentList";
import { useAgents } from "@/components/AgentsProvider";
import { ErrorState } from "@/components/ErrorState";
import { LoadingState } from "@/components/LoadingState";

export default function AgentListPage() {
  const { teams, loading, error } = useAgents();

  return (
    <>
      {error ? <ErrorState message={error} /> : <AgentList teams={teams || []} />}
      {loading && <LoadingState />}
    </>
  );
}
