import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { SystemPromptSection } from "@/components/create/SystemPromptSection";
import { ModelSelectionSection } from "@/components/create/ModelSelectionSection";
import type { ModelConfig } from "@/types";
import { storyModelConfigs } from "./fixtures";

function AgentCreateSectionsStory() {
  const [prompt, setPrompt] = useState(`You're a helpful agent.

# Instructions
- Ask for clarification when needed.
- Respond in Markdown.`);
  const [selectedModel, setSelectedModel] = useState<Partial<ModelConfig> | null>(storyModelConfigs[0] ?? null);

  return (
    <AppPageFrame ariaLabelledBy="agent-new-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <PageHeader titleId="agent-new-title" title="New Agent" className="mb-8" />
      <div className="space-y-10">
        <SystemPromptSection value={prompt} onChange={(e) => setPrompt(e.target.value)} disabled={false} />
        <ModelSelectionSection
          allModels={storyModelConfigs}
          selectedModel={selectedModel}
          setSelectedModel={setSelectedModel}
          isSubmitting={false}
          agentNamespace="default"
        />
      </div>
    </AppPageFrame>
  );
}

const meta = {
  title: "Pages/Create/Agent",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "`/agents/new` — representative sections (`SystemPromptSection`, `ModelSelectionSection`) with mock models. The full page adds tools, skills, memory, and submission.",
      },
    },
  },
  render: () => <AgentCreateSectionsStory />,
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const PrimarySections: Story = {};
