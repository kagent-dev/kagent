import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

const meta = {
  title: "Pages/Create/Model",
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "`/models/new` loads providers and models from the API. This story captures page chrome only; open the app for the full form.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

/** Matches page framing (narrow column, header) without server-backed fields. */
export const PageChrome: Story = {
  render: () => (
    <AppPageFrame ariaLabelledBy="model-new-title" mainClassName="mx-auto max-w-3xl px-4 py-10 sm:px-6">
      <PageHeader titleId="model-new-title" title="New Model" className="mb-8" />
      <Card>
        <CardHeader>
          <CardTitle>Basic Information</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Namespace, provider, model ID, and authentication sections appear here once providers load from the backend.
        </CardContent>
      </Card>
    </AppPageFrame>
  ),
};
