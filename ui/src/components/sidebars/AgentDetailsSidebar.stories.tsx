import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AgentDetailsSidebar } from "./AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import type { AgentResponse, Tool, ToolsResponse } from "@/types";

const meta: Meta<typeof AgentDetailsSidebar> = {
  title: "Sidebars/AgentDetailsSidebar",
  component: AgentDetailsSidebar,
  decorators: [
    (Story) => (
      <SidebarProvider defaultOpen={true}>
        <div className="flex h-screen w-full">
          <main className="flex-1 bg-background p-8">
            <p className="text-muted-foreground">Main content area</p>
          </main>
          <Story />
        </div>
      </SidebarProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof AgentDetailsSidebar>;

const mockAgent: AgentResponse = {
  id: 1,
  agent: {
    metadata: {
      name: "momus-gpt",
      namespace: "kagent",
    },
    spec: {
      description: "A helpful AI assistant for code review and analysis",
      type: "Declarative",
      skills: {
        refs: [
          "ghcr.io/kagent-dev/skill-code-analyzer:v1.0.0",
          "ghcr.io/kagent-dev/skill-documentation:v2.1.0",
        ],
      },
    },
  },
  model: "gpt-4",
  modelProvider: "openai",
  modelConfigRef: "openai-config",
  deploymentReady: true,
  accepted: true,
  tools: [
    {
      mcpServer: {
        name: "github-mcp",
        namespace: "kagent",
        toolNames: ["search_repos", "get_pr_details", "create_issue"],
      },
    } as Tool,
    {
      mcpServer: {
        name: "jira-mcp",
        namespace: "kagent",
        toolNames: ["search_issues", "create_ticket"],
      },
    } as Tool,
  ],
};

const mockAgentNoTools: AgentResponse = {
  id: 2,
  agent: {
    metadata: {
      name: "simple-agent",
      namespace: "kagent",
    },
    spec: {
      description: "A simple agent without tools",
      type: "Declarative",
    },
  },
  model: "gpt-3.5-turbo",
  modelProvider: "openai",
  modelConfigRef: "openai-config",
  deploymentReady: true,
  accepted: true,
  tools: [],
};

const mockBYOAgent: AgentResponse = {
  id: 3,
  agent: {
    metadata: {
      name: "custom-agent",
      namespace: "kagent",
    },
    spec: {
      description: "A bring-your-own agent",
      type: "BYO",
    },
  },
  model: "custom-model",
  modelProvider: "custom",
  modelConfigRef: "custom-config",
  deploymentReady: true,
  accepted: true,
  tools: [],
};

const mockTools: ToolsResponse[] = [
  {
    id: "search_repos",
    server_name: "kagent/github-mcp",
    description: "Search for repositories on GitHub",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "McpServer",
  },
  {
    id: "get_pr_details",
    server_name: "kagent/github-mcp",
    description: "Get detailed information about a pull request",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "McpServer",
  },
  {
    id: "create_issue",
    server_name: "kagent/github-mcp",
    description: "Create a new issue on GitHub",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "McpServer",
  },
  {
    id: "search_issues",
    server_name: "kagent/jira-mcp",
    description: "Search for issues in Jira",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "McpServer",
  },
  {
    id: "create_ticket",
    server_name: "kagent/jira-mcp",
    description: "Create a new ticket in Jira",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    deleted_at: "",
    group_kind: "McpServer",
  },
];

export const AgentWithTools: Story = {
  args: {
    selectedAgentName: "kagent/momus-gpt",
    currentAgent: mockAgent,
    allTools: mockTools,
  },
};

export const AgentWithNoTools: Story = {
  args: {
    selectedAgentName: "kagent/simple-agent",
    currentAgent: mockAgentNoTools,
    allTools: [],
  },
};

export const BYOAgent: Story = {
  args: {
    selectedAgentName: "kagent/custom-agent",
    currentAgent: mockBYOAgent,
    allTools: [],
  },
};
