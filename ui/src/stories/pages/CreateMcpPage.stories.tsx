import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AgentsContext } from "@/components/AgentsProvider";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { McpServerForm } from "@/components/mcp/McpServerForm";
import { createStoryAgentsContext } from "./fixtures";

const meta = {
  title: "Pages/Create/MCP server",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "`/mcp/new` — form shell with sample supported types (no create API).",
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
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Form: Story = {
  render: () => (
    <AppPageFrame ariaLabelledBy="mcp-new-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <div>
        <Link
          href="/mcp"
          className="mb-8 inline-flex items-center gap-2 rounded-sm text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden />
          Back to MCP & tools
        </Link>
        <PageHeader titleId="mcp-new-title" title="New MCP server" className="mb-8" />
        <McpServerForm supportedToolServerTypes={["RemoteMCPServer", "MCPServer"]} onCreate={async () => {}} />
      </div>
    </AppPageFrame>
  ),
};
