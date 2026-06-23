import { NextRequest } from "next/server";
import { mcpAppsBackendPath, optionsResponse, proxyMcpAppsRequest } from "@/app/api/mcp-apps/_utils";

export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ namespace: string; name: string; toolName: string }> },
) {
  const { namespace, name, toolName } = await params;
  const body = await request.text();
  return proxyMcpAppsRequest(
    request,
    `${mcpAppsBackendPath(namespace, name)}/tools/${encodeURIComponent(toolName)}/call`,
    {
      method: "POST",
      body,
    },
  );
}

export async function OPTIONS() {
  return optionsResponse();
}
