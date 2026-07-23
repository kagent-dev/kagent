import React from "react";
import { render, screen, waitFor } from "@testing-library/react";
import ChatLayoutUI from "@/components/chat/ChatLayoutUI";
import type { AgentResponse } from "@/types";

jest.mock("next/navigation", () => ({
  usePathname: () => "/agents/kagent/kanban-mcp-agent/chat",
  useParams: () => ({}),
}));

// Heavy descendants and server actions are stubbed; this test only verifies the
// layout wiring, not their behavior.
jest.mock("@/app/actions/sessions", () => ({
  getSessionsForAgent: jest.fn().mockResolvedValue({ data: [], error: null }),
}));
jest.mock("@/components/sidebars/SessionsSidebar", () => ({
  __esModule: true,
  default: () => <div data-testid="sessions-sidebar" />,
}));
jest.mock("@/components/sidebars/AgentDetailsSidebar", () => ({
  AgentDetailsSidebar: () => <div data-testid="agent-details-sidebar" />,
}));
jest.mock("@/components/chat/ChatAgentContext", () => ({
  ChatAgentProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

// Replace the real provider with a marker so we can assert ChatLayoutUI mounts
// it (and wraps the chat children with it). Without this provider the chat
// silently falls back to the empty default context and MCP App widgets never
// render — the regression this test guards against.
jest.mock("@/components/chat/ChatMcpAppsContext", () => ({
  ChatMcpAppsProvider: ({
    currentAgent,
    children,
  }: {
    currentAgent: AgentResponse;
    children: React.ReactNode;
  }) => (
    <div data-testid="mcp-apps-provider" data-agent={currentAgent.agent.metadata.name}>
      {children}
    </div>
  ),
}));

const currentAgent = {
  agent: {
    metadata: { namespace: "kagent", name: "kanban-mcp-agent" },
    spec: { type: "Declarative" },
  },
  workloadMode: "deployment",
} as unknown as AgentResponse;

function renderLayout() {
  return render(
    <ChatLayoutUI
      agentName="kanban-mcp-agent"
      namespace="kagent"
      currentAgent={currentAgent}
      allAgents={[currentAgent]}
      allTools={[]}
    >
      <div data-testid="chat-child">chat</div>
    </ChatLayoutUI>,
  );
}

describe("ChatLayoutUI", () => {
  it("mounts ChatMcpAppsProvider around the chat so MCP App widgets can render", async () => {
    renderLayout();

    const provider = await screen.findByTestId("mcp-apps-provider");
    expect(provider).toHaveAttribute("data-agent", "kanban-mcp-agent");
    // The chat content must live inside the provider, otherwise tool calls
    // never resolve to MCP App widgets.
    expect(provider).toContainElement(screen.getByTestId("chat-child"));
  });

  it("renders the chat children", async () => {
    renderLayout();
    await waitFor(() => expect(screen.getByTestId("chat-child")).toBeInTheDocument());
  });
});
