import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstance } from "@/api";
import { AgentContextPanel } from "./AgentContextPanel";

const {useAgentTemplate, useAgent} = vi.hoisted(() => ({useAgentTemplate: vi.fn(), useAgent: vi.fn()}));

vi.mock("@/api", () => ({
  useAgentTemplate,
  useAgent,
  bareName: (ref: string) => ref.split("/").pop(),
}));

function LocationState() {
  const location = useLocation();
  return <pre data-testid="location-state">{JSON.stringify(location.state)}</pre>;
}

function renderPanel(tools: unknown[], modelNamespace?: string, instance?: AgentInstance) {
  useAgent.mockReturnValue({data: {resource: {spec: {template: {ref: {name: "assistant"}}, harnessRef: {name: "runner"}}}}});
  useAgentTemplate.mockReturnValue({
    data: {
      resource: {
        spec: {
          modelConfig: { name: "gpt", namespace: modelNamespace },
          tools,
        },
      },
    },
    isLoading: false,
    error: undefined,
  });

  render(
    <ThemeProvider theme={themeFor("dark")}>
      <MemoryRouter initialEntries={["/agents/abc/chat?mock=ok"]}>
        <Routes>
          <Route
            path="/agents/:id/chat"
            element={
              <AgentContextPanel
                instance={instance}
                agentRef={{ namespace: "kagent", name: "assistant" }}
              />
            }
          />
          <Route path="/agent-templates/:namespace/:name" element={<LocationState />} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("AgentContextPanel", () => {
  it("opens the template with a way back to this conversation", async () => {
    renderPanel([], undefined, { agent: "kagent/assistant" } as AgentInstance);
    await userEvent.click(screen.getByTestId("chat-agent-context-template"));
    expect(JSON.parse(screen.getByTestId("location-state").textContent ?? "null")).toEqual({
      from: "/agents/abc/chat?mock=ok",
      fromLabel: "Back to conversation",
    });
  });

  it("says the way back leads to the agent when no conversation is open", async () => {
    renderPanel([]);
    await userEvent.click(screen.getByTestId("chat-agent-context-template"));
    expect(JSON.parse(screen.getByTestId("location-state").textContent ?? "null")).toMatchObject({
      fromLabel: "Back to agent",
    });
  });

  it("renders an MCP binding whose optional tools list is omitted", () => {
    renderPanel([
      {
        mcp: {
          server: { kind: "RemoteMCPServer", name: "tools" },
        },
      },
    ]);

    expect(screen.getByText("kagent/tools (all tools)")).toBeTruthy();
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

    expect(screen.getByText("kagent/tools: list_pods")).toBeTruthy();
  });
});

 it.each([undefined, "", "kagent", "shared"])("qualifies models and servers with namespace %s", (namespace) => {
   const resolved = namespace || "kagent";
   renderPanel([{ mcp: { server: { kind: "RemoteMCPServer", name: "tools", namespace } } }], namespace);
   expect(screen.getByText(`${resolved}/gpt`)).toBeTruthy();
   expect(screen.getByText(`${resolved}/tools (all tools)`)).toBeTruthy();
 });

 it("distinguishes identical tool names from different servers", () => {
   renderPanel(["kagent", "shared"].map((namespace) => ({ mcp: {
     server: { kind: "RemoteMCPServer", name: "tools", namespace }, tools: ["list_pods"],
   } })));
   expect(screen.getByText("kagent/tools: list_pods")).toBeTruthy();
   expect(screen.getByText("shared/tools: list_pods")).toBeTruthy();
 });
