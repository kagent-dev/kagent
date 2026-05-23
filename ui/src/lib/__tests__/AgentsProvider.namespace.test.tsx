import { render, screen, waitFor } from "@testing-library/react";
import { AgentsProvider } from "@/components/AgentsProvider";
import { getAgents } from "@/app/actions/agents";
import { getTools } from "@/app/actions/tools";
import { getModelConfigs } from "@/app/actions/modelConfigs";

jest.mock("@/app/actions/agents", () => ({
  getAgent: jest.fn(),
  createAgent: jest.fn(),
  getAgents: jest.fn(),
}));

jest.mock("@/app/actions/tools", () => ({
  getTools: jest.fn(),
}));

jest.mock("@/app/actions/modelConfigs", () => ({
  getModelConfigs: jest.fn(),
}));

const mockGetAgents = getAgents as jest.MockedFunction<typeof getAgents>;
const mockGetTools = getTools as jest.MockedFunction<typeof getTools>;
const mockGetModelConfigs = getModelConfigs as jest.MockedFunction<typeof getModelConfigs>;

describe("AgentsProvider list fetching", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGetTools.mockResolvedValue([]);
    mockGetModelConfigs.mockResolvedValue({
      message: "Successfully fetched models",
      data: [],
    });
  });

  it("does not fetch all agents on mount", async () => {
    render(
      <AgentsProvider>
        <div>provider child</div>
      </AgentsProvider>,
    );

    expect(screen.getByText("provider child")).toBeInTheDocument();
    await waitFor(() => expect(mockGetTools).toHaveBeenCalled());
    expect(mockGetAgents).not.toHaveBeenCalled();
  });
});
