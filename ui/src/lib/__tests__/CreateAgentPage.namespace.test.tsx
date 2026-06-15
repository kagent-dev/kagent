import { render, screen } from "@testing-library/react";
import { useRouter, useSearchParams } from "next/navigation";
import AgentPage from "@/app/agents/new/page";
import { SubstrateFeaturesTestProvider } from "@/contexts/SubstrateFeaturesContext";

jest.mock("next/navigation", () => ({
  useRouter: jest.fn(),
  useSearchParams: jest.fn(),
}));

jest.mock("@/components/AgentsProvider", () => ({
  useAgents: () => ({
    models: [],
    loading: false,
    error: "",
    createNewAgent: jest.fn(),
    updateAgent: jest.fn(),
    getAgent: jest.fn(),
    validateAgentData: jest.fn(() => ({})),
  }),
}));

jest.mock("@/components/NamespaceCombobox", () => ({
  NamespaceCombobox: ({ value }: { value?: string }) => (
    <div data-testid="namespace-value">{value}</div>
  ),
}));

jest.mock("@/components/create/SystemPromptSection", () => ({
  SystemPromptSection: () => null,
}));

jest.mock("@/components/create/ModelSelectionSection", () => ({
  ModelSelectionSection: () => null,
}));

jest.mock("@/components/create/ToolsSection", () => ({
  ToolsSection: () => null,
}));

jest.mock("@/components/create/MemorySection", () => ({
  MemorySection: () => null,
}));

jest.mock("@/components/create/ContextSection", () => ({
  ContextSection: () => null,
}));

jest.mock("@/components/agent-form/AgentSkillsFormSection", () => ({
  AgentSkillsFormSection: () => null,
}));

jest.mock("@/components/agent-form/ServiceAccountNameField", () => ({
  ServiceAccountNameField: () => null,
}));

jest.mock("@/components/agent-form/DeclarativeRuntimeField", () => ({
  DeclarativeRuntimeField: () => null,
}));

const mockUseRouter = useRouter as jest.Mock;
const mockUseSearchParams = useSearchParams as jest.Mock;

describe("new agent namespace query", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUseRouter.mockReturnValue({ push: jest.fn() });
  });

  it("initializes the editable namespace from ?namespace= in create mode", async () => {
    mockUseSearchParams.mockReturnValue(new URLSearchParams("namespace=%20kagent%20"));

    render(
      <SubstrateFeaturesTestProvider enabled={false}>
        <AgentPage />
      </SubstrateFeaturesTestProvider>,
    );

    expect(await screen.findByTestId("namespace-value")).toHaveTextContent(
      "kagent",
    );
  });
});
