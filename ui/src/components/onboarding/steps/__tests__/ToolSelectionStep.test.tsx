/**
 * @jest-environment jsdom
 *
 * Bug: handleToolToggle pushed a brand-new Tool object per checkbox click
 * instead of merging into an existing entry for the same MCP server (unlike
 * SelectToolsDialog.handleAddItem, which merges same-server picks into one
 * mcpServer.toolNames array). Selecting multiple tools from the same server
 * during onboarding produced several Tool entries that all shared the same
 * server identity - the data shape that made downstream duplicate-entry
 * rendering bugs possible (see ToolsSection's duplicate-key issue).
 *
 * Fix: handleToolToggle now merges same-server picks into a single entry on
 * select, and removes just that tool id (dropping the entry only once it
 * references no tools) on deselect.
 */
import React from "react";
import { describe, it, expect, jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ToolSelectionStep } from "@/components/onboarding/steps/ToolSelectionStep";
import type { Tool, ToolsResponse } from "@/types";

const makeTool = (
  id: string,
  serverName = "kagent/kagent-tool-server",
): ToolsResponse => ({
  id,
  server_name: serverName,
  created_at: "",
  updated_at: "",
  deleted_at: "",
  description: `${id} description`,
  group_kind: "Tool",
});

const renderStep = (tools: ToolsResponse[], onNext = jest.fn<(tools: Tool[]) => void>()) => {
  render(
    <ToolSelectionStep
      availableTools={tools}
      loadingTools={false}
      errorTools={null}
      initialSelectedTools={[]}
      onNext={onNext}
      onBack={jest.fn()}
    />,
  );
  return { onNext };
};

describe("ToolSelectionStep duplicate-selection prevention", () => {
  it("merges multiple tool picks from the same server into a single Tool entry", async () => {
    const user = userEvent.setup();
    const tools = [makeTool("k8s_get_pods"), makeTool("k8s_get_events")];
    const { onNext } = renderStep(tools);

    await user.click(await screen.findByRole("checkbox", { name: /k8s_get_pods/i }));
    await user.click(screen.getByRole("checkbox", { name: /k8s_get_events/i }));
    await user.click(screen.getByRole("button", { name: /next: review/i }));

    expect(onNext).toHaveBeenCalledTimes(1);
    const submitted = onNext.mock.calls[0][0];
    expect(submitted).toHaveLength(1);
    expect([...submitted[0]!.mcpServer!.toolNames].sort()).toEqual([
      "k8s_get_events",
      "k8s_get_pods",
    ]);
  });

  it("unchecking one of two same-server selections keeps the other tool selected", async () => {
    const user = userEvent.setup();
    const tools = [makeTool("k8s_get_pods"), makeTool("k8s_get_events")];
    const { onNext } = renderStep(tools);

    await user.click(await screen.findByRole("checkbox", { name: /k8s_get_pods/i }));
    await user.click(screen.getByRole("checkbox", { name: /k8s_get_events/i }));
    await user.click(screen.getByRole("checkbox", { name: /k8s_get_pods/i }));
    await user.click(screen.getByRole("button", { name: /next: review/i }));

    const submitted = onNext.mock.calls[0][0];
    expect(submitted).toHaveLength(1);
    expect(submitted[0]!.mcpServer!.toolNames).toEqual(["k8s_get_events"]);
  });

  it("unchecking the only selected tool for a server removes the entry entirely", async () => {
    const user = userEvent.setup();
    const tools = [makeTool("k8s_get_pods")];
    const { onNext } = renderStep(tools);

    await user.click(await screen.findByRole("checkbox", { name: /k8s_get_pods/i }));
    await user.click(screen.getByRole("checkbox", { name: /k8s_get_pods/i }));
    await user.click(screen.getByRole("button", { name: /next: review/i }));

    expect(onNext).toHaveBeenCalledWith([]);
  });

  it("keeps tools from different servers as separate Tool entries instead of merging them", async () => {
    // Both server names contain "kagent-tool-server" so ToolSelectionStep's
    // own K8s-only filter (server_name?.includes("kagent-tool-server"))
    // lets both through - but they have different parsed names, so
    // serverNamesMatch must NOT treat them as the same server.
    const user = userEvent.setup();
    const tools = [
      makeTool("k8s_get_pods", "kagent/kagent-tool-server"),
      makeTool("k8s_get_events", "kagent/kagent-tool-server-extra"),
    ];
    const { onNext } = renderStep(tools);

    await user.click(await screen.findByRole("checkbox", { name: /k8s_get_pods/i }));
    await user.click(screen.getByRole("checkbox", { name: /k8s_get_events/i }));
    await user.click(screen.getByRole("button", { name: /next: review/i }));

    expect(onNext).toHaveBeenCalledTimes(1);
    const submitted = onNext.mock.calls[0][0];
    expect(submitted).toHaveLength(2);

    const byServer = new Map(
      submitted.map((t) => [t!.mcpServer!.name, t!.mcpServer!.toolNames]),
    );
    expect(byServer.get("kagent-tool-server")).toEqual(["k8s_get_pods"]);
    expect(byServer.get("kagent-tool-server-extra")).toEqual(["k8s_get_events"]);
  });
});
