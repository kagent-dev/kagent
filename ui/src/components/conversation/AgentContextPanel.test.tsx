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
  autoTitle?: string,
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
      <MemoryRouter initialEntries={["/agents/abc/conversation?mock=ok"]}>
        <Routes>
          <Route
            path="/agents/:id/conversation"
            element={
              <AgentContextPanel
                instance={instance}
                autoTitle={autoTitle}
                agentRef={{ namespace: "kagent", name: "assistant" }}
                onShowDetails={onShowDetails}
              />
            }
          />
          <Route path="/agents/:namespace/:name" element={<LocationState />} />
          <Route path="/agent-templates/:namespace/:name" element={<LocationState />} />
          <Route path="/harnesses/:namespace/:name" element={<LocationState />} />
          <Route path="/models/:namespace/:name/edit" element={<LocationState />} />
          <Route path="/mcp-servers/:namespace/:name" element={<LocationState />} />
        </Routes>
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("AgentContextPanel", () => {
  it("names the agent template and the harness", () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    expect(screen.getByTestId("conversation-agent-context-template").textContent).toBe("kagent/helper-template");
    expect(screen.getByTestId("conversation-agent-context-harness").textContent).toBe("kagent/runner");
    expect(screen.getByText("Template")).toBeTruthy();
    expect(screen.getByText("Harness")).toBeTruthy();
    expect(screen.queryByText("Runs on")).toBeNull();
  });

  it("says an inline agent has no template, and why", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance, {
      inline: { modelConfig: { name: "gpt" }, tools: [] },
    });
    expect(screen.queryByTestId("conversation-agent-context-template")).toBeNull();
    expect(screen.getByText(/None\. The agent defines its own/)).toBeTruthy();
    await userEvent.hover(screen.getByTestId("conversation-agent-context-hint-template"));
    expect(await screen.findByText(/This agent has no AgentTemplate/)).toBeTruthy();
  });

  it("explains each label in a tooltip", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    await userEvent.hover(screen.getByTestId("conversation-agent-context-hint-harness"));
    expect(await screen.findByText(/How this agent runs/)).toBeTruthy();
  });

  it("puts the agent first, as a link to its page", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    const agent = screen.getByTestId("conversation-agent-context-agent");
    expect(screen.getByTestId("conversation-agent-context").querySelector("a")).toBe(agent);
    expect(agent.textContent).toBe("kagent/assistant");
    expect(screen.getAllByText("kagent/assistant")).toHaveLength(1);
    await userEvent.click(agent);
    expect(screen.getByTestId("location-path").textContent).toBe("/agents/kagent/assistant");
  });

  it("shows the conversation's name, ID and short revision, and opens its details", async () => {
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
    expect(screen.getByText("Conversation", { exact: true })).toBeTruthy();
    expect(screen.queryByRole("group")).toBeNull();
    expect(screen.queryByText(/\bchat\b/i)).toBeNull();
    expect(screen.getByTestId("conversation-agent-context-session-name").textContent).toBe("Cluster review");
    expect(screen.getByTestId("conversation-agent-context-session").textContent).toContain("6f1c9d20-1b7a");
    expect(screen.getByTestId("conversation-agent-context-revision").textContent).toContain("d825540fbe9f");
    expect(screen.getByTestId("conversation-agent-context-revision").textContent).not.toContain(revision);
    await userEvent.click(screen.getByRole("button", { name: /Conversation details/ }));
    expect(onShowDetails).toHaveBeenCalledOnce();
  });

  it.each([
    { autoTitle: "List the pods in the kagent namespace.", shown: "List the pods in the kagent namespace." },
    { autoTitle: undefined, shown: "Untitled" },
  ])("titles an unnamed conversation $shown, without repeating its id", ({ autoTitle, shown }) => {
    const id = "01a1ebcf-f6a7-702b-ab50-641bdbf8983f";
    renderPanel([], undefined, { id, name: "", agent: "kagent/assistant" } as AgentInstance,
      undefined, undefined, undefined, autoTitle);
    expect(screen.getByTestId("conversation-agent-context-session-name").textContent).toBe(shown);
    expect(screen.getByTestId("conversation-agent-context-session").textContent).toContain(id);
  });

  it("opens the template with a way back to this conversation", async () => {
    renderPanel([], undefined, { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance);
    await userEvent.click(screen.getByTestId("conversation-agent-context-template"));
    expect(JSON.parse(screen.getByTestId("location-state").textContent ?? "null")).toEqual({
      from: "/agents/abc/conversation?mock=ok",
      fromLabel: "Back to conversation",
    });
  });

  it("says the way back leads to the agent when no conversation is open", async () => {
    renderPanel([]);
    await userEvent.click(screen.getByTestId("conversation-agent-context-template"));
    expect(JSON.parse(screen.getByTestId("location-state").textContent ?? "null")).toMatchObject({
      fromLabel: "Back to agent",
    });
  });

  it.each([
    ["conversation-agent-context-harness", "/harnesses/kagent/runner"],
    ["conversation-agent-context-model", "/models/shared/gpt/edit"],
    ["conversation-agent-context-server", "/mcp-servers/kagent/tools?kind=RemoteMCPServer.api.kagent.dev"],
  ])("opens %s at %s with a way back to this conversation", async (testId, path) => {
    renderPanel(
      [{ mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["list_pods"] } }],
      "shared",
      { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance,
    );
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

    expect(screen.getByTestId("conversation-agent-context-server").textContent).toBe("kagent/tools");
    expect(screen.getByTestId("conversation-agent-context-server-count").textContent).toBe("All tools");
  });

  it("lists each tool with its description once opened, collapsed at first", async () => {
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
    expect(screen.getAllByTestId("conversation-agent-context-server")).toHaveLength(1);
    expect(screen.getByTestId("conversation-agent-context-server-count").textContent).toBe("2 of 3");

    const toggle = screen.getByTestId("conversation-agent-context-server-toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.getAllByTestId("conversation-agent-context-tool")[0].closest("[inert]")).not.toBeNull();
    await userEvent.click(toggle);
    const tools = screen.getAllByTestId("conversation-agent-context-tool");
    expect(tools.map((tool) => tool.querySelector("span")?.textContent)).toEqual(["list_pods", "get_logs"]);
    expect(tools[1].textContent).toContain("Does get_logs");
    // Tools are not links.
    expect(tools[1].querySelector("a")).toBeNull();

    await userEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(tools[0].closest("[inert]")).not.toBeNull();
  });

  it("expands and collapses every server at once, offered only when there is more than one", async () => {
    renderPanel([
      { mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["list_pods"] } },
      { mcp: { server: { kind: "RemoteMCPServer", name: "logs" }, tools: ["tail"] } },
    ]);
    const toggles = screen.getAllByTestId("conversation-agent-context-server-toggle");
    const all = screen.getByTestId("conversation-agent-context-servers-toggle-all");
    expect(toggles.map((t) => t.getAttribute("aria-expanded"))).toEqual(["false", "false"]);

    await userEvent.click(all);
    expect(toggles.map((t) => t.getAttribute("aria-expanded"))).toEqual(["true", "true"]);
    expect(all.textContent).toBe("Collapse all");

    // One closed by hand means not all are open, so the button offers to expand again.
    await userEvent.click(toggles[0]);
    expect(all.textContent).toBe("Expand all");
    await userEvent.click(all);
    await userEvent.click(all);
    expect(toggles.map((t) => t.getAttribute("aria-expanded"))).toEqual(["false", "false"]);
  });

  it("links a sub-agent to its template and says when the agent calls it", async () => {
    renderPanel(
      [{ subAgent: { name: "helm", description: "Helm specialist.", templateRef: { name: "platform-helm" } } }],
      undefined,
      { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance,
    );
    const link = screen.getByTestId("conversation-agent-context-subagent-link");
    expect(link.textContent).toBe("helm");
    expect(screen.getByTestId("conversation-agent-context-subagent-when").textContent).toBe("When it's calledHelm specialist.");
    await userEvent.hover(link);
    expect(await screen.findByText("An AgentTemplate this agent can call as a tool")).toBeTruthy();
    await userEvent.click(link);
    expect(screen.getByTestId("location-path").textContent).toBe("/agent-templates/kagent/platform-helm");
    expect(JSON.parse(screen.getByTestId("location-state").textContent ?? "null")).toMatchObject({ fromLabel: "Back to conversation" });
  });

  it("offers no expand-all for a single server", () => {
    renderPanel([{ mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["list_pods"] } }]);
    expect(screen.queryByTestId("conversation-agent-context-servers-toggle-all")).toBeNull();
  });

  it("toggles from the header and the keyboard, and opens the server from its name", async () => {
    renderPanel(
      [{ mcp: { server: { kind: "RemoteMCPServer", name: "tools" }, tools: ["list_pods"] } }],
      undefined,
      { id: "abc", name: "", agent: "kagent/assistant" } as AgentInstance,
    );
    const toggle = screen.getByTestId("conversation-agent-context-server-toggle");
    const link = screen.getByTestId("conversation-agent-context-server");
    expect(link.closest("button")).toBeNull();

    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    await userEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    toggle.focus();
    await userEvent.keyboard("{Enter}");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    await userEvent.keyboard(" ");
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(document.getElementById(toggle.getAttribute("aria-controls") ?? "")?.hasAttribute("inert")).toBe(false);

    await userEvent.click(link);
    expect(toggle.isConnected).toBe(false);
    expect(screen.getByTestId("location-path").textContent).toBe("/mcp-servers/kagent/tools?kind=RemoteMCPServer.api.kagent.dev");
  });
});

 it.each([undefined, "", "kagent", "shared"])("qualifies models and servers with namespace %s", (namespace) => {
   const resolved = namespace || "kagent";
   renderPanel([{ mcp: { server: { kind: "RemoteMCPServer", name: "tools", namespace } } }], namespace);
   expect(screen.getByText(`${resolved}/gpt`)).toBeTruthy();
   expect(screen.getByTestId("conversation-agent-context-server").textContent).toBe(`${resolved}/tools`);
 });

 it("keeps identical tool names from different servers apart", () => {
   renderPanel(["kagent", "shared"].map((namespace) => ({ mcp: {
     server: { kind: "RemoteMCPServer", name: "tools", namespace }, tools: ["list_pods"],
   } })));
   expect(screen.getAllByTestId("conversation-agent-context-server").map((link) => link.textContent)).toEqual([
     "kagent/tools",
     "shared/tools",
   ]);
   expect(screen.getAllByTestId("conversation-agent-context-server-count").map((count) => count.textContent)).toEqual([
     "1 tool",
     "1 tool",
   ]);
 });
