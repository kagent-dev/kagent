import { NextRequest } from "next/server";
import { mcpAppsBackendPath, optionsResponse, proxyMcpAppsRequest } from "@/app/api/mcp-apps/_utils";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ namespace: string; name: string }> },
) {
  const { namespace, name } = await params;
  const uri = request.nextUrl.searchParams.get("uri") || "";
  const path = `${mcpAppsBackendPath(namespace, name)}/resources?uri=${encodeURIComponent(uri)}`;
  return proxyMcpAppsRequest(request, path);
}

export async function OPTIONS() {
  return optionsResponse();
}
