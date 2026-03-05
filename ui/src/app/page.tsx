import AgentList from "@/components/AgentList";

export const dynamic = "force-dynamic";

export default async function AgentListPage() {
  const subtitle = process.env.NEXT_PUBLIC_SUBTITLE;
  return <AgentList subtitle={subtitle} />;
}
