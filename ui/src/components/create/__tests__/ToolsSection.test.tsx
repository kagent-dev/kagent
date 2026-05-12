/**
 * @jest-environment jsdom
 *
 * Test: ToolsSection form submit prevention
 *
 * Bug: The "Add Tools & Agents" trigger (empty- and populated-state variants)
 * and the per-tool remove (X) buttons inside <ToolsSection> were rendered
 * without an explicit `type` attribute. Inside a <form>, HTML defaults
 * `<button>` to type="submit", so clicking any of them on /agents/new
 * submitted the agent form and navigated away (creating a half-configured
 * Agent on new, or overwriting the existing Agent in edit mode).
 *
 * Fix: Set type="button" on the 4 affected buttons. The remove (X) buttons
 * additionally received aria-label values (with aria-hidden on the icon) so
 * screen readers can announce them and tests can target them by accessible
 * name.
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

  it("populated state (Agent ref): clicking the remove (X) button removes the tool and does NOT submit the surrounding form", async () => {
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

    const removeButton = await screen.findByRole("button", {
      name: /^remove tool$/i,
    });
    await user.click(removeButton);

    expect(onSubmit).not.toHaveBeenCalled();
    expect(setSelectedTools).toHaveBeenCalledTimes(1);
  });

  it("populated state (MCP tool): clicking the per-tool remove (X) button removes the binding and does NOT submit the surrounding form", async () => {
    const user = userEvent.setup();
    const tool: Tool = {
      type: "McpServer",
      mcpServer: {
        kind: "RemoteMCPServer",
        apiGroup: "kagent.dev",
        name: "context-forge",
        namespace: "kagent",
        toolNames: ["argocd-get-application"],
      },
    };
    const setSelectedTools = jest.fn();
    const { onSubmit } = renderInsideForm({
      selectedTools: [tool],
      setSelectedTools,
    });

    const removeButton = await screen.findByRole("button", {
      name: /^remove tool$/i,
    });
    await user.click(removeButton);

    expect(onSubmit).not.toHaveBeenCalled();
    expect(setSelectedTools).toHaveBeenCalledTimes(1);
  });
});
