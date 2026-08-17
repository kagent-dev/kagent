import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { mocked } from "storybook/test";
import { AgentsContext } from "@/components/AgentsProvider";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { McpServersView } from "@/components/mcp/McpServersView";
import { listMcpAppTools } from "@/app/actions/mcp-apps";
import { createStoryAgentsContext, storyMcpServers } from "./fixtures";

const storyMcpApps = [
  {
    name: "kanban_board",
    description: "Interactive kanban board for managing tasks",
    uiResourceUri: "ui://kanban/board",
  },
];

const meta = {
  title: "Pages/View/MCP & tools",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "`/mcp` — `McpServersView` with mock servers (no API).",
      },
    },
  },
  decorators: [
    (Story) => (
      <AgentsContext.Provider value={createStoryAgentsContext({})}>
        <Story />
      </AgentsContext.Provider>
    ),
  ],
  beforeEach: () => {
    mocked(listMcpAppTools).mockReset();
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = {
  beforeEach: () => {
    mocked(listMcpAppTools).mockResolvedValue({ message: "Tools fetched", data: storyMcpApps });
  },
  render: () => (
    <AppPageFrame ariaLabelledBy="mcp-page-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <PageHeader
        titleId="mcp-page-title"
        title="MCP & tools"
        description="Add MCP servers to your cluster, then search or expand each server to see the tools agents can use."
        className="mb-6"
      />
      <McpServersView servers={storyMcpServers} isLoading={false} loadError={null} onRefresh={async () => {}} />
    </AppPageFrame>
  ),
};

/** Apps count still resolving: server rows show a small spinner next to the tool count. */
export const LoadingApps: Story = {
  beforeEach: () => {
    mocked(listMcpAppTools).mockImplementation(() => new Promise(() => {}));
  },
  render: () => (
    <AppPageFrame ariaLabelledBy="mcp-page-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <PageHeader
        titleId="mcp-page-title"
        title="MCP & tools"
        description="Add MCP servers to your cluster, then search or expand each server to see the tools agents can use."
        className="mb-6"
      />
      <McpServersView servers={storyMcpServers} isLoading={false} loadError={null} onRefresh={async () => {}} />
    </AppPageFrame>
  ),
};

export const Loading: Story = {
  render: () => (
    <AppPageFrame ariaLabelledBy="mcp-page-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <PageHeader
        titleId="mcp-page-title"
        title="MCP & tools"
        description="Add MCP servers to your cluster, then search or expand each server to see the tools agents can use."
        className="mb-6"
      />
      <McpServersView servers={[]} isLoading loadError={null} onRefresh={async () => {}} />
    </AppPageFrame>
  ),
};

export const LoadError: Story = {
  render: () => (
    <AppPageFrame ariaLabelledBy="mcp-page-title" mainClassName="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
      <PageHeader
        titleId="mcp-page-title"
        title="MCP & tools"
        description="Add MCP servers to your cluster, then search or expand each server to see the tools agents can use."
        className="mb-6"
      />
      <McpServersView servers={[]} isLoading={false} loadError="Could not reach cluster API." onRefresh={async () => {}} />
    </AppPageFrame>
  ),
};
