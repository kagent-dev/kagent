"use client";

import { render, screen } from "@testing-library/react";
import AgentList from "@/components/AgentList";
import { AgentsContext, type AgentsContextType } from "@/components/AgentsProvider";
import type { Agent, AgentResponse } from "@/types";

const mockUseRouter = jest.fn();
const mockUsePathname = jest.fn();
const mockUseSearchParams = jest.fn();

jest.mock("next/navigation", () => ({
  useRouter: () => mockUseRouter(),
  usePathname: () => mockUsePathname(),
  useSearchParams: () => mockUseSearchParams(),
}));

function createContextValue(agents: AgentResponse[]): AgentsContextType {
  return {
    agents,
    models: [],
    loading: false,
    error: "",
    tools: [],
    refreshAgents: async () => {},
    refreshModels: async () => {},
    refreshTools: async () => {},
    createNewAgent: async () => ({ message: "ok", data: {} as Agent }),
    updateAgent: async () => ({ message: "ok", data: {} as Agent }),
    getAgent: async () => null,
    validateAgentData: () => ({}),
  };
}

const agents: AgentResponse[] = [
  {
    id: 1,
    agent: {
      metadata: { name: "support-bot", namespace: "kagent" },
      spec: {
        type: "Declarative",
        description: "Answers support questions",
      },
    },
    model: "gpt-4o",
    modelProvider: "openai",
    deploymentReady: true,
    accepted: true,
  },
  {
    id: 2,
    agent: {
      metadata: { name: "team-analyzer", namespace: "team-a" },
      spec: {
        type: "Declarative",
        description: "Analyzes incidents",
      },
    },
    model: "claude-sonnet",
    modelProvider: "anthropic",
    deploymentReady: true,
    accepted: true,
  },
];

describe("AgentList", () => {
  beforeEach(() => {
    mockUseRouter.mockReturnValue({
      push: jest.fn(),
      replace: jest.fn(),
      refresh: jest.fn(),
      back: jest.fn(),
    });
    mockUsePathname.mockReturnValue("/agents");
    mockUseSearchParams.mockReturnValue(new URLSearchParams());
  });

  afterEach(() => {
    jest.clearAllMocks();
  });

  it("filters agents by namespace from the URL", () => {
    mockUseSearchParams.mockReturnValue(new URLSearchParams("namespace=team-a"));

    render(
      <AgentsContext.Provider value={createContextValue(agents)}>
        <AgentList />
      </AgentsContext.Provider>,
    );

    expect(screen.getByLabelText("Namespace filter")).toBeInTheDocument();
    expect(screen.getByText("team-a/team-analyzer")).toBeInTheDocument();
    expect(screen.queryByText("kagent/support-bot")).not.toBeInTheDocument();
  });

  it("shows a filtered empty state when no agents match the namespace", () => {
    mockUseSearchParams.mockReturnValue(new URLSearchParams("namespace=team-b"));

    render(
      <AgentsContext.Provider value={createContextValue(agents)}>
        <AgentList />
      </AgentsContext.Provider>,
    );

    expect(screen.getByText("No agents in this namespace")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show all namespaces" })).toBeInTheDocument();
  });
});
