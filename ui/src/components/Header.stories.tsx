import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { Header } from "./Header";
import { AuthProvider } from "@/contexts/AuthContext";
import { SubstrateFeaturesTestProvider } from "@/contexts/SubstrateFeaturesContext";

const meta = {
  title: "Components/Header",
  component: Header,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <SubstrateFeaturesTestProvider enabled={false}>
        <AuthProvider>
          <Story />
        </AuthProvider>
      </SubstrateFeaturesTestProvider>
    ),
  ],
} satisfies Meta<typeof Header>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Mobile: Story = {
  parameters: {
    viewport: {
      defaultViewport: "mobile1",
    },
  },
};
