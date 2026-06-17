/**
 * @jest-environment jsdom
 */
import { render, screen } from "@testing-library/react";
import { AgentDetailsSidebar } from "@/components/sidebars/AgentDetailsSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import type { AgentResponse } from "@/types";

jest.mock("@/app/actions/agents", () => ({
  getAgents: jest.fn().mockResolvedValue({ data: [] }),
}));

function renderSidebar(currentAgent: AgentResponse) {
  return render(
    <SidebarProvider defaultOpen>
      <AgentDetailsSidebar
        currentAgent={currentAgent}
        allTools={[]}
      />
    </SidebarProvider>,
  );
}

const longNameAgent: AgentResponse = {
  id: 1,
  agent: {
    metadata: {
      name: "test-my-agent-qwen7b",
      namespace: "ak-poc-testing",
    },
    spec: {
      description: "testing me agent bro",
      type: "Declarative",
    },
  },
  model: "vllm/Qwen/Qwen2.5-7B-Instruct",
  modelProvider: "openai",
  modelConfigRef: "ak-poc-testing/qwen7b",
  deploymentReady: true,
  accepted: true,
  tools: [
    {
      type: "Agent",
      agent: {
        name: "k8s-agent",
        namespace: "ak-poc-testing",
        kind: "Agent",
        apiGroup: "kagent.dev",
      },
    },
  ],
};

describe("AgentDetailsSidebar", () => {
  it("shows the edit control in the header for agents with long names and model strings", () => {
    renderSidebar(longNameAgent);

    const editLink = screen.getByRole("link", {
      name: "Edit agent ak-poc-testing/test-my-agent-qwen7b",
    });
    expect(editLink).toBeInTheDocument();
    expect(editLink).toHaveAttribute(
      "href",
      "/agents/new?edit=true&name=test-my-agent-qwen7b&namespace=ak-poc-testing",
    );
  });

  it("renders agent ref and model on separate truncated lines", () => {
    renderSidebar(longNameAgent);

    expect(screen.getByText("ak-poc-testing/test-my-agent-qwen7b")).toBeInTheDocument();
    expect(screen.getByText("vllm/Qwen/Qwen2.5-7B-Instruct")).toBeInTheDocument();
  });
});
