import { getServers } from "@/app/actions/servers";
import { McpPageClient } from "@/components/mcp/McpPageClient";

export default async function McpPage() {
  const response = await getServers();
  return (
    <McpPageClient
      initialServers={response.data ?? []}
      initialError={response.error ?? null}
    />
  );
}
