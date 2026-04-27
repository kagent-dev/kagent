import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PromptLibrariesPanel } from "@/components/prompts/PromptLibrariesPanel";
import { storyPromptLibraries } from "./fixtures";

function PromptsStory({
  namespace,
  loading,
  empty,
}: {
  namespace: string;
  loading: boolean;
  empty: boolean;
}) {
  const items = loading ? null : empty ? [] : storyPromptLibraries;
  const [, setNs] = useState(namespace);
  return (
    <AppPageFrame ariaLabelledBy="prompts-page-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <PromptLibrariesPanel namespace={namespace} loading={loading} items={items} onNamespaceChange={setNs} />
    </AppPageFrame>
  );
}

const meta = {
  title: "Pages/View/Prompt Libraries",
  component: PromptsStory,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "`/prompts` — `PromptLibrariesPanel` with mock data.",
      },
    },
  },
} satisfies Meta<typeof PromptsStory>;

export default meta;
type Story = StoryObj<typeof PromptsStory>;

export const WithLibraries: Story = {
  args: { namespace: "kagent", loading: false, empty: false },
};

export const Loading: Story = {
  args: { namespace: "kagent", loading: true, empty: false },
};

export const EmptyNamespace: Story = {
  args: { namespace: "kagent", loading: false, empty: true },
};

export const ChooseNamespace: Story = {
  args: { namespace: "", loading: false, empty: false },
};
