/**
 * @jest-environment jsdom
 *
 * Bug: the "Selected Tools" badges rendered `tool.mcpServer?.name` (the MCP
 * server name, e.g. "kagent-tool-server") - one badge per array entry. Now
 * that ToolSelectionStep merges same-server picks into a single Tool entry's
 * mcpServer.toolNames array, this collapsed to a single, unhelpful badge no
 * matter how many/which tools were actually selected.
 *
 * Fix: flatMap over each entry's toolNames (for MCP tools) so Review shows
 * one badge per selected tool name instead of per array entry.
 */
import React from "react";
import { describe, it, expect } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { ReviewStep } from "@/components/onboarding/steps/ReviewStep";
import type { Tool } from "@/types";

describe("ReviewStep Selected Tools", () => {
  it("shows one badge per tool name, not one per (merged) server entry", () => {
    const mergedServerTool: Tool = {
      type: "McpServer",
      mcpServer: {
        kind: "RemoteMCPServer",
        apiGroup: "kagent.dev",
        name: "kagent-tool-server",
        namespace: "kagent",
        toolNames: ["k8s_get_pods", "k8s_get_events"],
      },
    };

    render(
      <ReviewStep
        onboardingData={{ selectedTools: [mergedServerTool] }}
        isLoading={false}
        onBack={() => {}}
        onSubmit={() => {}}
      />,
    );

    expect(screen.getByText("k8s_get_pods")).toBeInTheDocument();
    expect(screen.getByText("k8s_get_events")).toBeInTheDocument();
    expect(screen.queryByText("kagent-tool-server")).not.toBeInTheDocument();
  });

  it("shows tools from two different servers as separate entries, not merged", () => {
    const serverATool: Tool = {
      type: "McpServer",
      mcpServer: {
        kind: "RemoteMCPServer",
        apiGroup: "kagent.dev",
        name: "kagent-tool-server",
        namespace: "kagent",
        toolNames: ["k8s_get_pods"],
      },
    };
    const serverBTool: Tool = {
      type: "McpServer",
      mcpServer: {
        kind: "RemoteMCPServer",
        apiGroup: "kagent.dev",
        name: "context-forge",
        namespace: "kagent",
        toolNames: ["argocd-get-application"],
      },
    };

    render(
      <ReviewStep
        onboardingData={{ selectedTools: [serverATool, serverBTool] }}
        isLoading={false}
        onBack={() => {}}
        onSubmit={() => {}}
      />,
    );

    expect(screen.getByText("k8s_get_pods")).toBeInTheDocument();
    expect(screen.getByText("argocd-get-application")).toBeInTheDocument();
    expect(screen.queryByText("kagent-tool-server")).not.toBeInTheDocument();
    expect(screen.queryByText("context-forge")).not.toBeInTheDocument();
  });

  it("shows the agent name for Agent-type selections", () => {
    const agentTool: Tool = {
      type: "Agent",
      agent: { name: "researcher", namespace: "kagent" },
    };

    render(
      <ReviewStep
        onboardingData={{ selectedTools: [agentTool] }}
        isLoading={false}
        onBack={() => {}}
        onSubmit={() => {}}
      />,
    );

    expect(screen.getByText("researcher")).toBeInTheDocument();
  });
});
