import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { SystemPromptSection } from "./SystemPromptSection";
import type { PromptTemplateSummary } from "@/types";

const catalog: PromptTemplateSummary[] = [
  {
    namespace: "demo",
    name: "shared-lib",
    keyCount: 1,
    keys: ["base"],
  },
];

const meta = {
  title: "Create/SystemPromptSection",
  component: SystemPromptSection,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof SystemPromptSection>;

export default meta;
type Story = StoryObj<typeof meta>;

const baseArgs: Story["args"] = {
  value: "",
  onChange: () => {},
  disabled: false,
};

function WithMentionsWrapper() {
  const [value, setValue] = useState("Instructions with @ mentions enabled.");
  return (
    <div className="max-w-2xl p-6">
      <SystemPromptSection
        value={value}
        onChange={(e) => setValue(e.target.value)}
        disabled={false}
        mentionNamespace="demo"
        onPickInclude={() => {}}
        promptLibraryCatalogOverride={catalog}
      />
    </div>
  );
}

/** Mentions use live catalog fetch unless you wrap a custom story with catalog injection at PromptInstructionsTextarea level. */
export const WithMentions: Story = {
  args: {
    ...baseArgs,
    mentionNamespace: "demo",
    onPickInclude: () => {},
    promptLibraryCatalogOverride: catalog,
  },
  render: () => <WithMentionsWrapper />,
};

export const PlainTextarea: Story = {
  args: baseArgs,
  render: () => {
    const [value, setValue] = useState("No namespace — standard textarea only.");
    return (
      <div className="max-w-2xl p-6">
        <SystemPromptSection
          value={value}
          onChange={(e) => setValue(e.target.value)}
          disabled={false}
        />
      </div>
    );
  },
};
