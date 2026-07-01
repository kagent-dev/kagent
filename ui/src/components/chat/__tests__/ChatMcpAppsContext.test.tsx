import React from "react";
import { render, screen, waitFor } from "@testing-library/react";
import {
  ChatMcpAppsProvider,
  useChatMcpApps,
} from "@/components/chat/ChatMcpAppsContext";
import { listMcpAppTools } from "@/app/actions/mcp-apps";
import type { AgentResponse } from "@/types";

jest.mock("@/app/actions/mcp-apps", () => ({
  listMcpAppTools: jest.fn(),
}));

const mockedListMcpAppTools = listMcpAppTools as jest.Mock;

// An agent that selects all tools from a single RemoteMCPServer (kanban-mcp).
const currentAgent = {
  agent: {
    metadata: { namespace: "kagent", name: "assistant" },
  },
  tools: [
    {
      type: "McpServer",
      mcpServer: {
        kind: "RemoteMCPServer",
        apiGroup: "kagent.dev",
        name: "kanban-mcp",
        namespace: "kagent",
        toolNames: [], // empty => selects all tools
      },
    },
  ],
} as unknown as AgentResponse;

function Probe() {
  const { getMcpAppForTool, getMcpToolForAppCall } = useChatMcpApps();
  const board = getMcpAppForTool("show_board");
  const appOnly = getMcpAppForTool("internal_widget");
  const plain = getMcpAppForTool("plain_tool");
  const appOnlyTool = getMcpToolForAppCall("kagent", "kanban-mcp", "internal_widget");
  return (
    <div>
      <span data-testid="board">{board ? `${board.serverName}:${board.uiResourceUri}` : "none"}</span>
      <span data-testid="appOnly">{appOnly ? "yes" : "none"}</span>
      <span data-testid="plain">{plain ? "yes" : "none"}</span>
      <span data-testid="appOnlyTool">
        {appOnlyTool ? `${appOnlyTool.toolName}:${appOnlyTool.appOnly}` : "none"}
      </span>
    </div>
  );
}

describe("ChatMcpAppsProvider", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("registers agent-visible UI tools as apps and excludes app-only and non-UI tools", async () => {
    mockedListMcpAppTools.mockResolvedValue({
      error: false,
      data: [
        // Visible to both model and app -> agent-visible app.
        {
          name: "show_board",
          uiResourceUri: "ui://board",
          _meta: { ui: { visibility: ["model", "app"] } },
        },
        // App-only -> not surfaced via getMcpAppForTool, but resolvable for app calls.
        {
          name: "internal_widget",
          uiResourceUri: "ui://widget",
          _meta: { ui: { visibility: "app" } },
        },
        // No UI resource -> never an app.
        { name: "plain_tool" },
      ],
    });

    render(
      <ChatMcpAppsProvider currentAgent={currentAgent}>
        <Probe />
      </ChatMcpAppsProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("board").textContent).toBe("kanban-mcp:ui://board");
    });
    expect(screen.getByTestId("appOnly").textContent).toBe("none");
    expect(screen.getByTestId("plain").textContent).toBe("none");
    expect(screen.getByTestId("appOnlyTool").textContent).toBe("internal_widget:true");
    expect(mockedListMcpAppTools).toHaveBeenCalledWith("kagent", "kanban-mcp");
  });

  it("registers no apps when the agent has no MCP servers", async () => {
    const agentWithoutMcp = {
      agent: { metadata: { namespace: "kagent", name: "assistant" } },
      tools: [],
    } as unknown as AgentResponse;

    render(
      <ChatMcpAppsProvider currentAgent={agentWithoutMcp}>
        <Probe />
      </ChatMcpAppsProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("board").textContent).toBe("none");
    });
    expect(mockedListMcpAppTools).not.toHaveBeenCalled();
  });
});
