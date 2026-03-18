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

const createTokenStats = (stats: TokenStats): TokenStats => stats;

export const ZeroUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 0,
      prompt: 0,
      completion: 0,
    }),
  },
};

export const SmallUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 150,
      prompt: 50,
      completion: 100,
    }),
  },
};

export const MediumUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 2500,
      prompt: 1000,
      completion: 1500,
    }),
  },
};

export const LargeUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 50000,
      prompt: 20000,
      completion: 30000,
    }),
  },
};

export const VeryLargeUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 128000,
      prompt: 64000,
      completion: 64000,
    }),
  },
};

export const UnbalancedUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 5000,
      prompt: 4500,
      completion: 500,
    }),
  },
};

export const HighOutputUsage: Story = {
  args: {
    stats: createTokenStats({
      total: 8000,
      prompt: 1000,
      completion: 7000,
    }),
  },
};
