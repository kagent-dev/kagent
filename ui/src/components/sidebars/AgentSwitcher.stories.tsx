import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AgentSwitcher } from "./AgentSwitcher";
import { SidebarProvider, Sidebar, SidebarHeader } from "@/components/ui/sidebar";
import type { AgentResponse } from "@/types";

const meta: Meta<typeof AgentSwitcher> = {
  title: "Sidebars/AgentSwitcher",
  component: AgentSwitcher,
  decorators: [
    (Story) => (
      <SidebarProvider defaultOpen={true}>
        <div className="flex h-screen w-full">
          <Sidebar side="left" collapsible="none">
            <SidebarHeader>
              <Story />
            </SidebarHeader>
          </Sidebar>
          <main className="flex-1 bg-background p-8">
            <p className="text-muted-foreground">Main content area</p>
          </main>
        </div>
      </SidebarProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof AgentSwitcher>;

const mockAgent: AgentResponse = {
  id: 1,
  agent: {
    metadata: {
      name: "momus-gpt",
      namespace: "kagent",
    },
    spec: {
      description: "A helpful AI assistant",
      type: "Declarative",
    },
  },
  model: "gpt-4",
  modelProvider: "openai",
  modelConfigRef: "openai-config",
  deploymentReady: true,
  accepted: true,
  tools: [],
};

const mockAgent2: AgentResponse = {
  id: 2,
  agent: {
    metadata: {
      name: "code-reviewer",
      namespace: "kagent",
    },
    spec: {
      description: "Code review specialist",
      type: "Declarative",
    },
  },
  model: "gpt-4-turbo",
  modelProvider: "openai",
  modelConfigRef: "openai-config",
  deploymentReady: true,
  accepted: true,
  tools: [],
};

const mockAgent3: AgentResponse = {
  id: 3,
  agent: {
    metadata: {
      name: "data-analyst",
      namespace: "analytics",
    },
    spec: {
      description: "Data analysis and visualization",
      type: "Declarative",
    },
  },
  model: "claude-3-opus",
  modelProvider: "anthropic",
  modelConfigRef: "anthropic-config",
  deploymentReady: true,
  accepted: true,
  tools: [],
};

export const SingleAgent: Story = {
  args: {
    currentAgent: mockAgent,
    allAgents: [mockAgent],
  },
};

export const MultipleAgents: Story = {
  args: {
    currentAgent: mockAgent,
    allAgents: [mockAgent, mockAgent2, mockAgent3],
  },
};
