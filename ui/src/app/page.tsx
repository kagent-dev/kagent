import AgentList from "@/components/AgentList";

export const dynamic = "force-dynamic";

export default async function AgentListPage() {
  const subtitle = process.env.KAGENT_UI_SUBTITLE;
  return <AgentList subtitle={subtitle} />;
}
