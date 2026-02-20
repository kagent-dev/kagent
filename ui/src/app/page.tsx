import AgentList from "@/components/AgentList";

export default async function AgentListPage() {
  const subtitle = process.env.NEXT_PUBLIC_SUBTITLE;

  return <AgentList subtitle={subtitle} />;
}
