import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { ErrorState } from "./ErrorState";

const meta = {
  title: "Components/ErrorState",
  component: ErrorState,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ErrorState>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    message: "Failed to load agent configuration",
    showHomeButton: true,
  },
};

export const LongMessage: Story = {
  args: {
    message:
      "An unexpected error occurred while processing your request. The system encountered a critical failure in the database connection layer. Please check your network connectivity and try again. If the problem persists, contact support with error code: ERR_DB_CONNECTION_TIMEOUT_5432",
    showHomeButton: true,
  },
};

export const WithoutHomeButton: Story = {
  args: {
    message: "Session expired. Please log in again.",
    showHomeButton: false,
  },
};
