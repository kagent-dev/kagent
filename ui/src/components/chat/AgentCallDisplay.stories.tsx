import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import AgentCallDisplay from "./AgentCallDisplay";
import type { FunctionCall } from "@/types";

const meta = {
  title: "Components/AgentCallDisplay",
  component: AgentCallDisplay,
  parameters: {
    layout: "centered",
  },
} satisfies Meta<typeof AgentCallDisplay>;

export default meta;
type Story = StoryObj<typeof meta>;

const baseCall: FunctionCall = {
  id: "call-123",
  name: "data-processor__NS__default",
  args: {
    query: "analyze sales data",
    timeframe: "Q4 2024",
  },
};

export const Delegating: Story = {
  args: {
    call: baseCall,
    status: "requested",
  },
};

export const AwaitingResponse: Story = {
  args: {
    call: baseCall,
    status: "executing",
  },
};

export const Completed: Story = {
  args: {
    call: baseCall,
    status: "completed",
    result: {
      content: '{"summary": "Sales increased by 15% in Q4", "details": "Strong performance across all regions"}',
      is_error: false,
    },
  },
};

export const CompletedWithError: Story = {
  args: {
    call: baseCall,
    status: "completed",
    isError: true,
    result: {
      content: "Failed to connect to data source: Connection timeout after 30s",
      is_error: true,
    },
  },
};

export const LongAgentName: Story = {
  args: {
    call: {
      ...baseCall,
      name: "very_long_agent_name_with_many_words__NS__very_long_namespace_name",
    },
    status: "completed",
    result: {
      content: "Processing complete",
      is_error: false,
    },
  },
};

export const WithTimestamp: Story = {
  args: {
    call: baseCall,
    status: "completed",
    result: {
      content: "Task completed successfully",
      is_error: false,
    },
  },
};
