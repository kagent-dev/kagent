import { McpAppsInspector } from "@/components/mcp-apps/McpAppsInspector";

export default async function ServerMcpAppsPage({
  params,
}: {
  params: Promise<{ namespace: string; name: string }>;
}) {
  const { namespace, name } = await params;
  return <McpAppsInspector namespace={decodeURIComponent(namespace)} name={decodeURIComponent(name)} />;
}
