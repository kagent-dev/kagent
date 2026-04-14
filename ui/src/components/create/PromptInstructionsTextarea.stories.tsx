import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { useState } from "react";
import { PromptInstructionsTextarea } from "./PromptInstructionsTextarea";
import type { PromptTemplateSummary } from "@/types";

const sampleCatalog: PromptTemplateSummary[] = [
  {
    namespace: "demo",
    name: "team-prompts",
    keyCount: 2,
    keys: ["system", "router"],
  },
  {
    namespace: "demo",
    name: "kagent-builtin-prompts",
    keyCount: 1,
    keys: ["default"],
  },
];

function StatefulPromptArea(props: {
  catalogOverride?: PromptTemplateSummary[];
  namespace: string;
}) {
  const [value, setValue] = useState("You are an agent in {{.AgentNamespace}}.\n\n");
  return (
    <div className="max-w-2xl p-6">
      <PromptInstructionsTextarea
        value={value}
        onChange={(e) => setValue(e.target.value)}
        disabled={false}
        namespace={props.namespace}
        onPickInclude={() => {}}
        includeSourceIdForConfigMap={(name) =>
          name === "kagent-builtin-prompts" ? "builtin" : name
        }
        catalogOverride={props.catalogOverride}
      />
    </div>
  );
}

const meta = {
  title: "Create/PromptInstructionsTextarea",
  component: PromptInstructionsTextarea,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof PromptInstructionsTextarea>;

export default meta;
type Story = StoryObj<typeof meta>;

const baseArgs: Story["args"] = {
  value: "",
  onChange: () => {},
  disabled: false,
  namespace: "demo",
  onPickInclude: () => {},
};

export const Default: Story = {
  args: {
    ...baseArgs,
    catalogOverride: sampleCatalog,
  },
  render: () => <StatefulPromptArea namespace="demo" catalogOverride={sampleCatalog} />,
};

export const NoNamespace: Story = {
  args: {
    ...baseArgs,
    namespace: "",
    catalogOverride: [],
  },
  render: () => {
    const [value, setValue] = useState("Type @ after setting a namespace…");
    return (
      <div className="max-w-2xl p-6">
        <PromptInstructionsTextarea
          value={value}
          onChange={(e) => setValue(e.target.value)}
          disabled={false}
          namespace=""
          onPickInclude={() => {}}
          catalogOverride={[]}
        />
      </div>
    );
  },
};
