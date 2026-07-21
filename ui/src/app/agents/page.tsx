import AgentList from "@/components/AgentList";
import { getAgents } from "@/app/actions/agents";

export default async function AgentListPage({
  searchParams,
}: {
  searchParams: Promise<{ namespace?: string }>;
}) {
  const { namespace = "" } = await searchParams;
  const response = await getAgents(namespace.trim() ? { namespace: namespace.trim() } : {});
  return <AgentList initialAgents={response.data ?? []} initialError={response.error ?? ""} />;
}
