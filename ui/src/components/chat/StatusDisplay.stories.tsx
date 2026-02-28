import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import StatusDisplay from "./StatusDisplay";
import type { ChatStatus } from "@/types";

const meta = {
  title: "Chat/StatusDisplay",
  component: StatusDisplay,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof StatusDisplay>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Ready: Story = {
  args: {
    chatStatus: "ready" as ChatStatus,
  },
};

export const Thinking: Story = {
  args: {
    chatStatus: "thinking" as ChatStatus,
  },
};

export const Error: Story = {
  args: {
    chatStatus: "error" as ChatStatus,
  },
};

export const ErrorWithMessage: Story = {
  args: {
    chatStatus: "error" as ChatStatus,
    errorMessage: "Failed to process request. Please try again.",
  },
};

export const Submitted: Story = {
  args: {
    chatStatus: "submitted" as ChatStatus,
  },
};

export const Working: Story = {
  args: {
    chatStatus: "working" as ChatStatus,
  },
};

export const InputRequired: Story = {
  args: {
    chatStatus: "input_required" as ChatStatus,
  },
};

export const AuthRequired: Story = {
  args: {
    chatStatus: "auth_required" as ChatStatus,
  },
};

export const ProcessingTools: Story = {
  args: {
    chatStatus: "processing_tools" as ChatStatus,
  },
};

export const GeneratingResponse: Story = {
  args: {
    chatStatus: "generating_response" as ChatStatus,
  },
};

export const ErrorWithCustomMessage: Story = {
  args: {
    chatStatus: "error" as ChatStatus,
    errorMessage: "Connection timeout. The server did not respond in time.",
  },
};

export const ErrorWithLongMessage: Story = {
  args: {
    chatStatus: "error" as ChatStatus,
    errorMessage: "An unexpected error occurred while processing your request. Please check your input and try again.",
  },
};
