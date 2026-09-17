import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { AgentContextPanel } from "./AgentContextPanel";

const useAgentTemplate = vi.hoisted(() => vi.fn());

vi.mock("@/api", () => ({ useAgentTemplate }));

function renderPanel(tools: unknown[]) {
  useAgentTemplate.mockReturnValue({
    data: {
      resource: {
        spec: {
          modelConfig: { name: "gpt" },
          tools,
        },
      },
    },
    isLoading: false,
    error: undefined,
  });

  render(
    <ThemeProvider theme={themeFor("dark")}>
      <MemoryRouter>
        <AgentContextPanel
          pair={{ namespace: "kagent", agentTemplate: "assistant", harness: "runner" }}
        />
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("AgentContextPanel", () => {
  it("renders an MCP binding whose optional tools list is omitted", () => {
    renderPanel([
      {
        mcp: {
          server: { kind: "RemoteMCPServer", name: "tools" },
        },
      },
    ]);

    expect(screen.getByText("tools (all tools)")).toBeTruthy();
  });

  it("renders explicitly selected MCP tools by name", () => {
    renderPanel([
      {
        mcp: {
          server: { kind: "RemoteMCPServer", name: "tools" },
          tools: ["list_pods"],
        },
      },
    ]);

    expect(screen.getByText("list_pods")).toBeTruthy();
  });
});
