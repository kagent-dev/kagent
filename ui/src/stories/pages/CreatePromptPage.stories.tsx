import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { PromptLibraryCreatePanel } from "@/components/prompts/PromptLibraryCreatePanel";
import { rowsFromData, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";

function PromptCreateStory() {
  const [namespace, setNamespace] = useState("kagent");
  const [name, setName] = useState("team-prompts");
  const [rows, setRows] = useState<FragmentRow[]>(() =>
    rowsFromData({
      intro: "You are a helpful assistant.",
      closing: "Thanks for using kagent.",
    }),
  );
  return (
    <PromptLibraryCreatePanel
      namespace={namespace}
      name={name}
      rows={rows}
      saving={false}
      backHref={`/prompts?namespace=${encodeURIComponent(namespace)}`}
      cancelHref={`/prompts?namespace=${encodeURIComponent(namespace)}`}
      onNamespaceChange={setNamespace}
      onNameChange={setName}
      onRowsChange={setRows}
      onSubmit={(e) => e.preventDefault()}
    />
  );
}

const meta = {
  title: "Pages/Create/Prompt Library",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "`/prompts/new` — `PromptLibraryCreatePanel` with local state (namespace combobox may still request APIs when opened).",
      },
    },
  },
  render: () => <PromptCreateStory />,
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Form: Story = {};
