import {
  listMcpAppTools,
  callMcpAppTool,
  readMcpAppResource,
} from "@/app/actions/mcp-apps";
import { getToolGrpcGateway } from "@/lib/grpc/client";

jest.mock("@/app/actions/utils", () => ({
  createErrorResponse: jest.fn((err: unknown, message: string) => ({
    error: true,
    message,
  })),
}));

jest.mock("@/lib/grpc/client", () => ({
  getToolGrpcGateway: jest.fn(),
}));

const listMcpAppToolsGateway = jest.fn();
const callMcpAppToolGateway = jest.fn();
const readMcpAppResourceGateway = jest.fn();
const mockedGetToolGrpcGateway = getToolGrpcGateway as jest.MockedFunction<typeof getToolGrpcGateway>;

describe("mcp-apps server actions", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    listMcpAppToolsGateway.mockResolvedValue([]);
    callMcpAppToolGateway.mockResolvedValue({ content: [] });
    readMcpAppResourceGateway.mockResolvedValue({ contents: [] });
    mockedGetToolGrpcGateway.mockResolvedValue({
      listMcpAppTools: listMcpAppToolsGateway,
      callMcpAppTool: callMcpAppToolGateway,
      readMcpAppResource: readMcpAppResourceGateway,
    } as never);
  });

  it("lists tools for the namespaced server", async () => {
    listMcpAppToolsGateway.mockResolvedValueOnce([{ name: "move_task" }]);

    const result = await listMcpAppTools("kagent", "kanban-mcp");

    expect(listMcpAppToolsGateway).toHaveBeenCalledWith("kagent", "kanban-mcp", undefined);
    expect(result).toEqual({
      message: "Successfully listed MCP app tools",
      data: [{ name: "move_task" }],
    });
  });

  it("calls tools with arguments and the selected CRD group kind", async () => {
    callMcpAppToolGateway.mockResolvedValueOnce({ content: [{ type: "text", text: "moved" }] });

    const result = await callMcpAppTool(
      "kagent",
      "kanban-mcp",
      "move_task",
      { id: "t1", to: "done" },
      "RemoteMCPServer.kagent.dev",
    );

    expect(callMcpAppToolGateway).toHaveBeenCalledWith(
      "kagent",
      "kanban-mcp",
      "move_task",
      { id: "t1", to: "done" },
      "RemoteMCPServer.kagent.dev",
    );
    expect(result.data).toEqual({ content: [{ type: "text", text: "moved" }] });
  });

  it("passes omitted arguments through for the gateway default", async () => {
    await callMcpAppTool("kagent", "kanban-mcp", "refresh");

    expect(callMcpAppToolGateway).toHaveBeenCalledWith(
      "kagent",
      "kanban-mcp",
      "refresh",
      undefined,
      undefined,
    );
  });

  it("reads a resource through the selected CRD", async () => {
    readMcpAppResourceGateway.mockResolvedValueOnce({ contents: [{ uri: "ui://board" }] });

    const result = await readMcpAppResource(
      "kagent",
      "kanban-mcp",
      "ui://board?x=1",
      "MCPServer.kagent.dev",
    );

    expect(readMcpAppResourceGateway).toHaveBeenCalledWith(
      "kagent",
      "kanban-mcp",
      "ui://board?x=1",
      "MCPServer.kagent.dev",
    );
    expect(result.data).toEqual({ contents: [{ uri: "ui://board" }] });
  });

  it("returns an error response when the gRPC gateway throws", async () => {
    listMcpAppToolsGateway.mockRejectedValueOnce(new Error("boom"));

    const result = await listMcpAppTools("kagent", "kanban-mcp");

    expect(result).toEqual({ error: true, message: "Failed to list MCP app tools" });
  });
});
