import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstance } from "@/api";
import { AgentContextPanel } from "./AgentContextPanel";

const {useAgentTemplate, useAgent, useMcpServers} = vi.hoisted(() => ({
  useAgentTemplate: vi.fn(),
  useAgent: vi.fn(),
  useMcpServers: vi.fn(),
}));

vi.mock("@/api", () => ({
  useAgentTemplate,
  useAgent,
  useMcpServers,
  bareName: (ref: string) => ref.split("/").pop(),
}));

function LocationState() {
  const location = useLocation();
  return (
    <>
      <pre data-testid="location-path">{location.pathname + location.search + location.hash}</pre>
      <pre data-testid="location-state">{JSON.stringify(location.state)}</pre>
    </>
  );
}

function renderPanel(
  tools: unknown[],
  modelNamespace?: string,
  instance?: AgentInstance,
  template: unknown = { ref: { name: "helper-template" } },
  servers: unknown[] = [],
  onShowDetails?: () => void,
) {
  useMcpServers.mockReturnValue({ data: servers });
  useAgent.mockReturnValue({data: {resource: {spec: {template, harnessRef: {name: "runner"}}}}});
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
                onShowDetails={onShowDetails}
              />
            }
          />
          <Route path="/agents/:namespace/:name" element={<LocationState />} />
          <Route path="/agent-templates/:namespace/:name" element={<LocationState />} />
          <Route path="/harnesses/:namespace/:name" element={<LocationState />} />
          <Route path="/models/:namespace/:name/edit" element={<LocationState />} />
          <Route path="/mcp/:namespace/:name" element={<LocationState />} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("AgentContextPanel", () => {
  it("names the agent template and the harness", () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    expect(screen.getByTestId("chat-agent-context-template").textContent).toBe("kagent/helper-template");
    expect(screen.getByTestId("chat-agent-context-harness").textContent).toBe("kagent/runner");
    expect(screen.getByText("Agent template")).toBeTruthy();
    expect(screen.getByText("Harness")).toBeTruthy();
    expect(screen.queryByText("Runs on")).toBeNull();
  });

  it("says an inline agent has no template, and why", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance, {
      inline: { modelConfig: { name: "gpt" }, tools: [] },
    });
    expect(screen.queryByTestId("chat-agent-context-template")).toBeNull();
    expect(screen.getByText(/None\. The agent defines its own/)).toBeTruthy();
    await userEvent.hover(screen.getByTestId("chat-agent-context-hint-agent-template"));
    expect(await screen.findByText(/This agent has no AgentTemplate/)).toBeTruthy();
  });

  it("explains each label in a tooltip", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    await userEvent.hover(screen.getByTestId("chat-agent-context-hint-harness"));
    expect(await screen.findByText(/runtime that executes this agent/)).toBeTruthy();
  });

  it("puts the agent first, as a link to its page", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    const agent = screen.getByTestId("chat-agent-context-agent");
    expect(screen.getByTestId("chat-agent-context").querySelector("a")).toBe(agent);
    expect(agent.textContent).toBe("kagent/assistant");
    expect(screen.getAllByText("kagent/assistant")).toHaveLength(1);
    await userEvent.click(agent);
    expect(screen.getByTestId("location-path").textContent).toBe("/agents/kagent/assistant");
  });

  it("shows the chat's name, ID and short revision, and opens its details", async () => {
    const revision = "d825540fbe9fbbb38259ace153f7712286baa762bf097b3ca8cc7097c119847b";
    const onShowDetails = vi.fn();
    renderPanel(
      [],
      undefined,
      { id: "6f1c9d20-1b7a", name: "Cluster review", agent: "kagent/assistant", preparedRevision: revision } as AgentInstance,
      undefined,
      undefined,
      onShowDetails,
    );
    expect(screen.getByRole("group", { name: "Chat" })).toBeTruthy();
    expect(screen.queryByText(/conversation/i)).toBeNull();
    expect(screen.getByTestId("chat-agent-context-session-name").textContent).toBe("Cluster review");
    expect(screen.getByTestId("chat-agent-context-session").textContent).toContain("6f1c9d20-1b7a");
    expect(screen.getByTestId("chat-agent-context-revision").textContent).toContain("d825540fbe9f");
    expect(screen.getByTestId("chat-agent-context-revision").textContent).not.toContain(revision);
    await userEvent.click(screen.getByRole("button", { name: /Chat details/ }));
    expect(onShowDetails).toHaveBeenCalledOnce();
  });

  it("opens the template with a way back to this conversation", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
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

  it.each([
    ["chat-agent-context-harness", "/harnesses/kagent/runner"],
    ["chat-agent-context-model", "/models/shared/gpt/edit"],
    ["chat-agent-context-server", "/mcp/kagent/tools?kind=RemoteMCPServer.api.kagent.dev"],
  ])("opens %s at %s with a way back to this conversation", async (testId, path) => {
    renderPanel(
      [{ mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["list_pods"] } }],
      "shared",
      { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance,
    );
    await userEvent.click(screen.getByTestId("chat-agent-context-server-toggle"));
    await userEvent.click(screen.getByTestId(testId));
    expect(screen.getByTestId("location-path").textContent).toBe(path);
    expect(JSON.parse(screen.getByTestId("location-state").textContent ?? "null")).toMatchObject({
      fromLabel: "Back to conversation",
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

    expect(screen.getByTestId("chat-agent-context-server").textContent).toBe("kagent/tools");
    expect(screen.getByTestId("chat-agent-context-server-count").textContent).toBe("All tools");
  });

  it("groups tools under their server with a selected-of-total count, collapsed until opened", async () => {
    renderPanel(
      [
        { mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["list_pods"] } },
        { mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["get_logs", "list_pods"] } },
      ],
      undefined,
      undefined,
      undefined,
      [{
        ref: "kagent/tools",
        groupKind: "RemoteMCPServer.api.kagent.dev",
        discoveredTools: ["list_pods", "get_logs", "describe"].map((name) => ({ name, description: `Does ${name}` })),
      }],
    );
    expect(screen.getAllByTestId("chat-agent-context-server")).toHaveLength(1);
    expect(screen.getByTestId("chat-agent-context-server-count").textContent).toBe("2/3 tools");

    const toggle = screen.getByTestId("chat-agent-context-server-toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    await userEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByTestId("chat-agent-context-tool").map((tool) => tool.textContent)).toEqual([
      "list_pods",
      "get_logs",
    ]);

    // Tools are not links; focus shows what each one does.
    const tool = screen.getAllByTestId("chat-agent-context-tool")[1];
    expect(tool.closest("a")).toBeNull();
    tool.focus();
    expect(await screen.findByText("Does get_logs")).toBeTruthy();
  });
});

 it.each([undefined, "", "kagent", "shared"])("qualifies models and servers with namespace %s", (namespace) => {
   const resolved = namespace || "kagent";
   renderPanel([{ mcp: { server: { kind: "RemoteMCPServer", name: "tools", namespace } } }], namespace);
   expect(screen.getByText(`${resolved}/gpt`)).toBeTruthy();
   expect(screen.getByTestId("chat-agent-context-server").textContent).toBe(`${resolved}/tools`);
 });

 it("keeps identical tool names from different servers apart", () => {
   renderPanel(["kagent", "shared"].map((namespace) => ({ mcp: {
     server: { kind: "RemoteMCPServer", name: "tools", namespace }, tools: ["list_pods"],
   } })));
   expect(screen.getAllByTestId("chat-agent-context-server").map((link) => link.textContent)).toEqual([
     "kagent/tools",
     "shared/tools",
   ]);
   expect(screen.getAllByTestId("chat-agent-context-server-count").map((count) => count.textContent)).toEqual([
     "1 tool",
     "1 tool",
   ]);
 });
