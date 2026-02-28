import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AgentCard } from "./AgentCard";
import type { AgentResponse } from "@/types";

const meta = {
  title: "Components/AgentCard",
  component: AgentCard,
  parameters: {
    layout: "centered",
  },
} satisfies Meta<typeof AgentCard>;

export default meta;
type Story = StoryObj<typeof meta>;

const baseAgentResponse: AgentResponse = {
  id: 1,
  agent: {
    metadata: {
      name: "my-agent",
      namespace: "default",
    },
    spec: {
      type: "Declarative",
      description: "A helpful assistant for answering questions",
    },
  },
  model: "gpt-4",
  modelProvider: "openai",
  modelConfigRef: "openai-config",
  tools: [],
  deploymentReady: true,
  accepted: true,
};

export const Ready: Story = {
  args: {
    agentResponse: baseAgentResponse,
  },
};

export const NotReady: Story = {
  args: {
    agentResponse: {
      ...baseAgentResponse,
      deploymentReady: false,
    },
  },
};

export const NotAccepted: Story = {
  args: {
    agentResponse: {
      ...baseAgentResponse,
      accepted: false,
    },
  },
};

export const BYOAgent: Story = {
  args: {
    agentResponse: {
      ...baseAgentResponse,
      agent: {
        ...baseAgentResponse.agent,
        spec: {
          ...baseAgentResponse.agent.spec,
          type: "BYO",
          byo: {
            deployment: {
              image: "my-registry.azurecr.io/my-agent:v1.0.0",
            },
          },
        },
      },
    },
  },
};

export const LongDescription: Story = {
  args: {
    agentResponse: {
      ...baseAgentResponse,
      agent: {
        ...baseAgentResponse.agent,
        spec: {
          ...baseAgentResponse.agent.spec,
          description:
            "This is a very long description that explains in great detail what this agent does. It can handle multiple tasks including data analysis, report generation, and customer support. The agent is equipped with advanced tools and integrations to provide comprehensive solutions.",
        },
      },
    },
  },
};

export const LongName: Story = {
  args: {
    agentResponse: {
      ...baseAgentResponse,
      agent: {
        ...baseAgentResponse.agent,
        metadata: {
          name: "very-long-agent-name-that-exceeds-normal-length",
          namespace: "very-long-namespace-name",
        },
      },
    },
  },
};
