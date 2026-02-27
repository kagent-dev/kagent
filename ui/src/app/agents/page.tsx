import AgentList from "@/components/AgentList";
export default function AgentListPage() {
  const subtitle = process.env.NEXT_PUBLIC_SUBTITLE;
  return <AgentList subtitle={subtitle} />;
}
