/**
 * @jest-environment jsdom
 *
 * Bug: on the BYO agent path, blurring the "Container image" field always
 * reported "Container image is required", even when a valid, non-empty
 * value was already present (e.g. when editing an existing BYO agent).
 *
 * Root cause: validateField's "model" case (used as the shared error slot
 * for both Declarative's model select and BYO's container image field)
 * only wrote the value into formData.modelName. For BYO agents,
 * validateAgentFormData actually checks formData.byoImage, which was left
 * undefined regardless of the real input value - so the check
 * `!data.byoImage?.trim()` was unconditionally true.
 *
 * Fix: the "model" case now writes the value into both formData.modelName
 * and formData.byoImage, since only one of them is actually inspected,
 * depending on state.agentType.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRouter, useSearchParams } from "next/navigation";
import AgentPage from "@/app/agents/new/page";
import { SubstrateFeaturesTestProvider } from "@/contexts/SubstrateFeaturesContext";
import { validateAgentFormData } from "@/lib/agentFormDomain";
import type { AgentResponse } from "@/types";

jest.mock("next/navigation", () => ({
  useRouter: jest.fn(),
  useSearchParams: jest.fn(),
}));

const mockGetAgent = jest.fn();

jest.mock("@/components/AgentsProvider", () => ({
  useAgents: () => ({
    models: [],
    loading: false,
    error: "",
    createNewAgent: jest.fn(),
    updateAgent: jest.fn(),
    getAgent: mockGetAgent,
    validateAgentData: (
      data: Partial<import("@/lib/agentFormDomain").AgentFormData>,
    ) => validateAgentFormData(data),
  }),
}));

jest.mock("@/components/NamespaceCombobox", () => ({
  NamespaceCombobox: () => null,
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

const byoAgentResponse: AgentResponse = {
  id: 1,
  agent: {
    metadata: { name: "my-byo-agent", namespace: "kagent" },
    spec: {
      type: "BYO",
      description: "",
      byo: {
        deployment: {
          image: "ghcr.io/org/agent:v1.0.0",
        },
      },
    },
  } as AgentResponse["agent"],
  model: "",
  modelProvider: "",
  modelConfigRef: "",
  tools: [],
  deploymentReady: true,
  accepted: true,
  workloadMode: "deployment",
};

describe("BYO container image validation on blur (edit mode)", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUseRouter.mockReturnValue({ push: jest.fn() });
    mockUseSearchParams.mockReturnValue(
      new URLSearchParams(
        "edit=true&name=my-byo-agent&namespace=kagent",
      ),
    );
    mockGetAgent.mockResolvedValue(byoAgentResponse);
  });

  it("does not report 'Container image is required' when a value is already present, on first blur", async () => {
    const user = userEvent.setup();

    render(
      <SubstrateFeaturesTestProvider enabled={false}>
        <AgentPage />
      </SubstrateFeaturesTestProvider>,
    );

    const imageInput = await screen.findByDisplayValue(
      "ghcr.io/org/agent:v1.0.0",
    );

    imageInput.focus();
    await user.tab();

    expect(
      screen.queryByText("Container image is required"),
    ).not.toBeInTheDocument();
  });
});
