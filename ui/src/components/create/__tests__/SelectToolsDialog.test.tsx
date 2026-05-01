/**
 * @jest-environment jsdom
 */
import { describe, expect, it, jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SelectToolsDialog } from "@/components/create/SelectToolsDialog";
import type { AgentResponse, Tool, ToolsResponse } from "@/types";

const serverRef = "kagent/kagent-tool-server";

const makeTool = (id: string): ToolsResponse => ({
  id,
  server_name: serverRef,
  description: `Description for ${id}`,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  deleted_at: "",
  group_kind: "MCPServer.kagent.dev",
});

const makeSelectedTools = (count: number): Tool[] => [
  {
    type: "McpServer",
    mcpServer: {
      name: "kagent-tool-server",
      namespace: "kagent",
      kind: "MCPServer",
      apiGroup: "kagent.dev",
      toolNames: Array.from({ length: count }, (_, i) => `tool_${i}`),
    },
  },
];

const renderOpenDialog = (selectedTools: Tool[], availableTools: ToolsResponse[]) => {
  const props = {
    open: false,
    onOpenChange: jest.fn(),
    availableTools,
    selectedTools,
    onToolsSelected: jest.fn(),
    availableAgents: [] as AgentResponse[],
    loadingAgents: false,
    currentAgentNamespace: "kagent",
  };

  const view = render(<SelectToolsDialog {...props} />);
  view.rerender(<SelectToolsDialog {...props} open />);
  return props;
};

describe("SelectToolsDialog", () => {
  it("warns but does not block selection when many tools are selected", async () => {
    const user = userEvent.setup();
    const availableTools = Array.from({ length: 21 }, (_, i) => makeTool(`tool_${i}`));

    renderOpenDialog(makeSelectedTools(20), availableTools);

    expect(screen.getByText("Selected (20)")).toBeInTheDocument();
    expect(screen.getByText(/You have selected 20 tools/i)).toBeInTheDocument();
    expect(screen.queryByText(/Tool limit reached/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Select up to/i)).not.toBeInTheDocument();

    await user.click(screen.getByText("tool_20"));

    expect(screen.getByText("Selected (21)")).toBeInTheDocument();
    expect(screen.getByText(/You have selected 21 tools/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Save Selection \(21\)/i })).toBeInTheDocument();
  });
});
