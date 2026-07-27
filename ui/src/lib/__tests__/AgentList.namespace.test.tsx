import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRouter, useSearchParams } from "next/navigation";
import AgentList from "@/components/AgentList";
import { getAgents } from "@/app/actions/agents";
import type { AgentResponse } from "@/types";

jest.mock("@/app/actions/agents", () => ({
  getAgents: jest.fn(),
}));

jest.mock("@/components/NamespaceCombobox", () => ({
  NamespaceCombobox: ({
    value,
    onValueChange,
  }: {
    value?: string;
    onValueChange: (value: string) => void;
  }) => (
    <select
      aria-label="Namespace"
      value={value || ""}
      onChange={(event) => onValueChange(event.target.value)}
    >
      <option value="">All namespaces</option>
      <option value="kagent">kagent</option>
      <option value="kube-system">kube-system</option>
    </select>
  ),
}));

jest.mock("next/navigation", () => ({
  useRouter: jest.fn(),
  useSearchParams: jest.fn(),
}));

const mockGetAgents = getAgents as jest.MockedFunction<typeof getAgents>;
const mockUseRouter = useRouter as jest.Mock;
const mockUseSearchParams = useSearchParams as jest.Mock;

function agent(namespace: string, name: string): AgentResponse {
  return {
    id: `${namespace}/${name}`,
    agent: {
      metadata: { namespace, name },
      spec: {
        type: "Declarative",
        description: `${name} description`,
      },
    },
    model: "gpt-4.1-mini",
    modelProvider: "OpenAI",
    modelConfigRef: `${namespace}/model`,
    tools: [],
    deploymentReady: true,
    accepted: true,
  };
}

function creatingAgent(namespace: string, name: string): AgentResponse {
  return {
    ...agent(namespace, name),
    deploymentReady: false,
  };
}

function setup(search = "") {
  const push = jest.fn();
  mockUseRouter.mockReturnValue({ push });
  mockUseSearchParams.mockReturnValue(new URLSearchParams(search));
  return { push };
}

describe("AgentList namespace filtering", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGetAgents.mockResolvedValue({
      message: "Successfully fetched agents",
      data: [agent("kagent", "k8s-agent")],
    });
  });

  it("renders server-loaded agents without a duplicate client fetch", async () => {
    setup();

    render(<AgentList initialAgents={[agent("kagent", "k8s-agent")]} />);

    expect(mockGetAgents).not.toHaveBeenCalled();
    expect(
      await screen.findByText("Showing agents across all namespaces."),
    ).toBeInTheDocument();
  });

  it("renders namespace-scoped server data from the namespace URL query", async () => {
    setup("namespace=kagent");

    render(<AgentList initialAgents={[agent("kagent", "k8s-agent")]} />);

    expect(mockGetAgents).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/Showing agents in namespace/i),
    ).toBeInTheDocument();
  });

  it("updates the URL when the namespace selector changes", async () => {
    const user = userEvent.setup();
    const { push } = setup();

    render(<AgentList />);

    await user.selectOptions(await screen.findByLabelText("Namespace"), "kagent");

    expect(push).toHaveBeenCalledWith("/agents?namespace=kagent");
  });

  it("clears the namespace query when All namespaces is selected", async () => {
    const user = userEvent.setup();
    const { push } = setup("namespace=kagent");

    render(<AgentList />);

    await user.selectOptions(await screen.findByLabelText("Namespace"), "");

    expect(push).toHaveBeenCalledWith("/agents");
  });

  it("renders scoped empty state with a namespace-aware create link", async () => {
    setup("namespace=kube-system");
    render(<AgentList initialAgents={[]} />);

    expect(
      await screen.findByText('No agents found in namespace "kube-system".'),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /new agent/i })).toHaveAttribute(
      "href",
      "/agents/new?namespace=kube-system",
    );
  });

  it("refreshes a transitional agent until it becomes ready", async () => {
    jest.useFakeTimers();
    try {
      setup();
      mockGetAgents.mockResolvedValue({
        message: "Successfully fetched agents",
        data: [agent("kagent", "k8s-agent")],
      });

      render(<AgentList initialAgents={[creatingAgent("kagent", "k8s-agent")]} />);

      expect(screen.getByText("Agent not Ready")).toBeInTheDocument();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(5000);
      });

      expect(mockGetAgents).toHaveBeenCalledWith({});
      expect(screen.queryByText("Agent not Ready")).not.toBeInTheDocument();
      expect(screen.getByRole("link", { name: /kagent\/k8s-agent/i })).toHaveAttribute(
        "href",
        "/agents/kagent/k8s-agent/chat",
      );
    } finally {
      jest.useRealTimers();
    }
  });
});
