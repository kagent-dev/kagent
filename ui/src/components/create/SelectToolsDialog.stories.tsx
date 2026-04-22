import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { SelectToolsDialog } from "./SelectToolsDialog";
import type { AgentResponse, Tool, ToolsResponse } from "@/types";

const toolServer = "kagent/kagent-tool-server";

const availableTools: ToolsResponse[] = [
  {
    id: "cilium_get_endpoint_logs",
    server_name: toolServer,
    description: "Get the logs of an endpoint in the cluster",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "MCPServer.kagent.dev",
  },
  {
    id: "other_tool",
    server_name: toolServer,
    description: "Another tool on the same server",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "MCPServer.kagent.dev",
  },
];

const availableAgents: AgentResponse[] = [
  {
    id: 1,
    agent: {
      metadata: { name: "argo-rollouts-conversion-agent", namespace: "kagent" },
      spec: {
        description:
          "The Argo Rollouts Converter AI Agent specializes in converting Kubernetes Deployments to Argo Rollouts.",
        type: "Declarative",
      },
    },
    model: "gpt-4",
    modelProvider: "openai",
    modelConfigRef: "default",
    tools: [],
    deploymentReady: true,
    accepted: true,
  },
];

const mcpToolSelected: Tool = {
  type: "McpServer",
  mcpServer: {
    name: "kagent-tool-server",
    namespace: "kagent",
    kind: "MCPServer",
    apiGroup: "kagent.dev",
    toolNames: ["cilium_get_endpoint_logs", "other_tool"],
    requireApproval: ["cilium_get_endpoint_logs"],
  },
};

const agentToolSelected: Tool = {
  type: "Agent",
  agent: {
    name: "argo-rollouts-conversion-agent",
    namespace: "kagent",
    kind: "Agent",
    apiGroup: "kagent.dev",
  },
};

function DialogHarness({
  initialSelected,
  availableToolsOverride,
  availableAgentsOverride,
}: {
  initialSelected: Tool[];
  availableToolsOverride?: ToolsResponse[];
  availableAgentsOverride?: AgentResponse[];
}) {
  const [open, setOpen] = useState(true);
  const [selected, setSelected] = useState<Tool[]>(initialSelected);

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Dialog stays open for visual checks. Use the story controls or reopen from the button if you close it.
      </p>
      <button
        type="button"
        className="w-fit rounded-md border px-3 py-1.5 text-sm"
        onClick={() => setOpen(true)}
      >
        Open dialog
      </button>
      <SelectToolsDialog
        open={open}
        onOpenChange={setOpen}
        availableTools={availableToolsOverride ?? availableTools}
        selectedTools={selected}
        onToolsSelected={setSelected}
        availableAgents={availableAgentsOverride ?? availableAgents}
        loadingAgents={false}
        currentAgentNamespace="kagent"
      />
    </div>
  );
}

const meta: Meta<typeof SelectToolsDialog> = {
  title: "Create/SelectToolsDialog",
  component: SelectToolsDialog,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-[90vh] w-full bg-background p-6">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof SelectToolsDialog>;

/** MCP tools in the right-hand list with “Require approval” on a separate row; one tool has approval enabled. */
export const McpToolsWithRequireApproval: Story = {
  render: () => <DialogHarness initialSelected={[mcpToolSelected]} />,
};

/** Selected MCP tools plus an agent-as-tool row (green icon) — approval controls only on MCP rows. */
export const MixedMcpAndAgentSelected: Story = {
  render: () => <DialogHarness initialSelected={[mcpToolSelected, agentToolSelected]} />,
};

/** Stress long titles and descriptions in the selected panel (truncation / line-clamp). */
export const LongTextSelected: Story = {
  render: () => {
    const longTools: ToolsResponse[] = [
      {
        id: "very_long_tool_name_that_should_wrap_and_not_break_layout",
        server_name: "kagent/very-long-mcp-server-name-for-storybook",
        description:
          "This is an intentionally long description that should clamp to a single line in the selected-tools panel and show a tooltip on hover for the full text when supported.",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
        deleted_at: "",
        group_kind: "MCPServer.kagent.dev",
      },
    ];
    const longMcp: Tool = {
      type: "McpServer",
      mcpServer: {
        name: "very-long-mcp-server-name-for-storybook",
        namespace: "kagent",
        kind: "MCPServer",
        apiGroup: "kagent.dev",
        toolNames: ["very_long_tool_name_that_should_wrap_and_not_break_layout"],
        requireApproval: [],
      },
    };
    return <DialogHarness initialSelected={[longMcp]} availableToolsOverride={longTools} />;
  },
};
