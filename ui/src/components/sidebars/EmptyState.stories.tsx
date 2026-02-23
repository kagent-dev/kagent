import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { EmptyState } from "./EmptyState";
import { SidebarProvider } from "@/components/ui/sidebar";

const meta: Meta<typeof EmptyState> = {
  title: "Sidebars/EmptyState",
  component: EmptyState,
  decorators: [
    (Story) => (
      <SidebarProvider defaultOpen={true}>
        <div className="h-screen w-full">
          <Story />
        </div>
      </SidebarProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof EmptyState>;

export const Default: Story = {};
