import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { ModelsListSection } from "@/components/models/ModelsListSection";
import { storyModelConfigs } from "./fixtures";

function ModelsPageStory({ expanded }: { expanded: boolean }) {
  const [expandedRows, setExpandedRows] = useState<Set<string>>(() => {
    if (!expanded || storyModelConfigs.length === 0) {
      return new Set();
    }
    return new Set([storyModelConfigs[0].ref]);
  });

  return (
    <AppPageFrame ariaLabelledBy="models-list-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <div>
        <PageHeader
          titleId="models-list-title"
          title="Models"
          description="Model configs, providers, and credentials that agents use at runtime."
          className="mb-8"
          end={
            <Button type="button" variant="default" className="w-full sm:w-auto" size="lg">
              <Plus className="mr-2 h-4 w-4" aria-hidden />
              New Model
            </Button>
          }
        />

        <ModelsListSection
          models={storyModelConfigs}
          expandedRows={expandedRows}
          onToggleRow={(ref) => {
            setExpandedRows((prev) => {
              const next = new Set(prev);
              if (next.has(ref)) {
                next.delete(ref);
              } else {
                next.add(ref);
              }
              return next;
            });
          }}
          onEdit={() => {}}
          onRequestDelete={() => {}}
          modelToDelete={null}
          onDismissDeleteDialog={() => {}}
          onConfirmDelete={() => {}}
          onNewModel={() => {}}
        />
      </div>
    </AppPageFrame>
  );
}

const meta = {
  title: "Pages/View/Models",
  component: ModelsPageStory,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "`/models` table and empty state (see Empty story) using `ModelsListSection`.",
      },
    },
  },
} satisfies Meta<typeof ModelsPageStory>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithRows: Story = {
  args: { expanded: false },
};

export const ExpandedFirstRow: Story = {
  args: { expanded: true },
};

export function Empty() {
  const [expandedRows] = useState<Set<string>>(() => new Set());
  return (
    <AppPageFrame ariaLabelledBy="models-list-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <div>
        <PageHeader
          titleId="models-list-title"
          title="Models"
          description="Model configs, providers, and credentials that agents use at runtime."
          className="mb-8"
          end={
            <Button type="button" variant="default" className="w-full sm:w-auto" size="lg">
              <Plus className="mr-2 h-4 w-4" aria-hidden />
              New Model
            </Button>
          }
        />
        <ModelsListSection
          models={[]}
          expandedRows={expandedRows}
          onToggleRow={() => {}}
          onEdit={() => {}}
          onRequestDelete={() => {}}
          modelToDelete={null}
          onDismissDeleteDialog={() => {}}
          onConfirmDelete={() => {}}
          onNewModel={() => {}}
        />
      </div>
    </AppPageFrame>
  );
}
