import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import TokenStatsDisplay from "./TokenStats";
import type { TokenStats } from "@/types";

const meta = {
  title: "Chat/TokenStats",
  component: TokenStatsDisplay,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof TokenStatsDisplay>;

export default meta;
type Story = StoryObj<typeof meta>;

export const ZeroUsage: Story = {
  args: {
    stats: {
      total: 0,
      input: 0,
      output: 0,
    } as TokenStats,
  },
};

export const SmallUsage: Story = {
  args: {
    stats: {
      total: 150,
      input: 50,
      output: 100,
    } as TokenStats,
  },
};

export const MediumUsage: Story = {
  args: {
    stats: {
      total: 2500,
      input: 1000,
      output: 1500,
    } as TokenStats,
  },
};

export const LargeUsage: Story = {
  args: {
    stats: {
      total: 50000,
      input: 20000,
      output: 30000,
    } as TokenStats,
  },
};

export const VeryLargeUsage: Story = {
  args: {
    stats: {
      total: 128000,
      input: 64000,
      output: 64000,
    } as TokenStats,
  },
};

export const UnbalancedUsage: Story = {
  args: {
    stats: {
      total: 5000,
      input: 4500,
      output: 500,
    } as TokenStats,
  },
};

export const HighOutputUsage: Story = {
  args: {
    stats: {
      total: 8000,
      input: 1000,
      output: 7000,
    } as TokenStats,
  },
};
