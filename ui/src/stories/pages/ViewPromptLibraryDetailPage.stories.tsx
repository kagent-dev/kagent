import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { PromptLibraryEditorPanel } from "@/components/prompts/PromptLibraryEditorPanel";
import { rowsFromData, type FragmentRow } from "@/components/prompts/FragmentEntriesEditor";

function PromptDetailStory() {
  const [rows, setRows] = useState<FragmentRow[]>(() =>
    rowsFromData({
      system: "You are a concise assistant.",
      tools: "Prefer built-in tools when possible.",
    }),
  );
  const [confirmOpen, setConfirmOpen] = useState(false);

  return (
    <PromptLibraryEditorPanel
      namespace="kagent"
      name="shared-lib"
      rows={rows}
      saving={false}
      confirmOpen={confirmOpen}
      listHref="/prompts?namespace=kagent"
      onRowsChange={setRows}
      onSave={() => {}}
      onDeleteClick={() => setConfirmOpen(true)}
      onConfirmDelete={() => setConfirmOpen(false)}
      onConfirmOpenChange={setConfirmOpen}
    />
  );
}

const meta = {
  title: "Pages/View/Prompt Library detail",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "`/prompts/[namespace]/[name]` — `PromptLibraryEditorPanel` with mock rows.",
      },
    },
  },
  render: () => <PromptDetailStory />,
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Editor: Story = {};
