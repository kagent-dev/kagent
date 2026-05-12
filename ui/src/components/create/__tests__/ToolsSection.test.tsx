/**
 * @jest-environment jsdom
 */
import React from "react";
import { describe, it, expect, jest, beforeEach } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ToolsSection } from "@/components/create/ToolsSection";
import type { Tool } from "@/types";

jest.mock("@/app/actions/agents", () => ({
  getAgents: jest.fn(async () => ({ error: undefined, data: [] })),
}));
jest.mock("@/app/actions/tools", () => ({
  getTools: jest.fn(async () => []),
}));
jest.mock("@/components/create/SelectToolsDialog", () => ({
  SelectToolsDialog: () => null,
}));

const renderInsideForm = (
  props: Partial<React.ComponentProps<typeof ToolsSection>> = {},
) => {
  const onSubmit = jest.fn((e: React.FormEvent) => {
    e.preventDefault();
  });
  const setSelectedTools = jest.fn();

  const utils = render(
    <form onSubmit={onSubmit}>
      <ToolsSection
        selectedTools={[]}
        setSelectedTools={setSelectedTools}
        isSubmitting={false}
        currentAgentName=""
        currentAgentNamespace="kagent"
        {...props}
      />
    </form>,
  );

  return { ...utils, onSubmit, setSelectedTools };
};

describe("ToolsSection inside a <form>", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("empty state: clicking 'Add Tools & Agents' does NOT submit the surrounding form", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderInsideForm();

    const button = await screen.findByRole("button", {
      name: /add tools & agents/i,
    });
    await user.click(button);

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("populated state: clicking the header 'Add Tools & Agents' button does NOT submit the surrounding form", async () => {
    const user = userEvent.setup();
    const tool: Tool = {
      type: "Agent",
      agent: { name: "another-agent", namespace: "kagent" },
    };
    const { onSubmit } = renderInsideForm({ selectedTools: [tool] });

    const button = await screen.findByRole("button", {
      name: /add tools & agents/i,
    });
    await user.click(button);

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("populated state: clicking a tool's remove (X) button removes the tool and does NOT submit the surrounding form", async () => {
    const user = userEvent.setup();
    const tool: Tool = {
      type: "Agent",
      agent: { name: "another-agent", namespace: "kagent" },
    };
    const setSelectedTools = jest.fn();
    const { onSubmit } = renderInsideForm({
      selectedTools: [tool],
      setSelectedTools,
    });

    await screen.findByRole("button", { name: /add tools & agents/i });

    const buttons = screen.getAllByRole("button");
    const removeButton = buttons[buttons.length - 1];

    await user.click(removeButton);

    expect(onSubmit).not.toHaveBeenCalled();
    expect(setSelectedTools).toHaveBeenCalledTimes(1);
  });
});
