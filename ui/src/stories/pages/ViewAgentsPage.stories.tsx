import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AgentsContext } from "@/components/AgentsProvider";
import AgentList from "@/components/AgentList";
import { createStoryAgentsContext, storyAgentResponses } from "./fixtures";

const meta = {
  title: "Pages/View/Agents",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "Agents home (`/` and `/agents`): list/grid from `AgentList` with mocked context.",
      },
    },
  },
  decorators: [
    (Story) => (
      <AgentsContext.Provider value={createStoryAgentsContext({ agents: storyAgentResponses, loading: false })}>
        <Story />
      </AgentsContext.Provider>
    ),
  ],
  render: () => <AgentList />,
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Loading: Story = {
  decorators: [
    (Story) => (
      <AgentsContext.Provider value={createStoryAgentsContext({ agents: [], loading: true })}>
        <Story />
      </AgentsContext.Provider>
    ),
  ],
};
