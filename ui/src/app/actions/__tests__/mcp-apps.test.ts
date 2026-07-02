import {
  listMcpAppTools,
  callMcpAppTool,
  readMcpAppResource,
} from "@/app/actions/mcp-apps";
import { fetchApi } from "@/app/actions/utils";

jest.mock("@/app/actions/utils", () => ({
  fetchApi: jest.fn(),
  createErrorResponse: jest.fn((err: unknown, message: string) => ({
    error: true,
    message,
  })),
}));

const mockedFetchApi = fetchApi as jest.Mock;

describe("mcp-apps server actions", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedFetchApi.mockResolvedValue({ error: false, data: [] });
  });

  it("lists tools for the namespaced server", async () => {
    await listMcpAppTools("kagent", "kanban-mcp");

    expect(mockedFetchApi).toHaveBeenCalledWith("/mcp-apps/kagent/kanban-mcp/tools");
  });

  it("URL-encodes namespace and server names", async () => {
    await listMcpAppTools("my ns", "weird/name");

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/my%20ns/weird%2Fname/tools"
    );
  });

  it("POSTs tool calls with a JSON arguments body", async () => {
    await callMcpAppTool("kagent", "kanban-mcp", "move_task", { id: "t1", to: "done" });

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/kagent/kanban-mcp/tools/move_task/call",
      {
        method: "POST",
        body: JSON.stringify({ arguments: { id: "t1", to: "done" } }),
      }
    );
  });

  it("defaults tool-call arguments to an empty object", async () => {
    await callMcpAppTool("kagent", "kanban-mcp", "refresh");

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/kagent/kanban-mcp/tools/refresh/call",
      {
        method: "POST",
        body: JSON.stringify({ arguments: {} }),
      }
    );
  });

  it("reads a resource by URI (encoded)", async () => {
    await readMcpAppResource("kagent", "kanban-mcp", "ui://board?x=1");

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/kagent/kanban-mcp/resources?uri=ui%3A%2F%2Fboard%3Fx%3D1"
    );
  });

  it("appends groupKind so the backend resolves the right CRD", async () => {
    await listMcpAppTools("kagent", "kanban-mcp", "MCPServer.kagent.dev");

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/kagent/kanban-mcp/tools?groupKind=MCPServer.kagent.dev"
    );
  });

  it("appends groupKind on tool calls", async () => {
    await callMcpAppTool("kagent", "kanban-mcp", "refresh", undefined, "RemoteMCPServer.kagent.dev");

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/kagent/kanban-mcp/tools/refresh/call?groupKind=RemoteMCPServer.kagent.dev",
      {
        method: "POST",
        body: JSON.stringify({ arguments: {} }),
      }
    );
  });

  it("appends groupKind after the resource uri query", async () => {
    await readMcpAppResource("kagent", "kanban-mcp", "ui://board", "MCPServer.kagent.dev");

    expect(mockedFetchApi).toHaveBeenCalledWith(
      "/mcp-apps/kagent/kanban-mcp/resources?uri=ui%3A%2F%2Fboard&groupKind=MCPServer.kagent.dev"
    );
  });

  it("returns an error response when fetchApi throws", async () => {
    mockedFetchApi.mockRejectedValueOnce(new Error("boom"));

    const result = await listMcpAppTools("kagent", "kanban-mcp");

    expect(result).toEqual({ error: true, message: "Failed to list MCP app tools" });
  });
});
