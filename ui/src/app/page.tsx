import AgentList from "@/components/AgentList";
import { getAgents } from "@/app/actions/agents";

export default async function AgentListPage() {
  const response = await getAgents();
  return <AgentList initialAgents={response.data ?? []} initialError={response.error ?? ""} />;
}
