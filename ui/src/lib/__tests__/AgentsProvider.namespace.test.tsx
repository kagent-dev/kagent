import { act, render, screen, waitFor } from "@testing-library/react";
import { AgentsProvider, useAgents } from "@/components/AgentsProvider";
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

// A tiny consumer that surfaces the two pieces of provider state we assert on:
// the shared `error` string and the list of model configs.
function ModelsConsumer() {
  const { error, models } = useAgents();
  return (
    <div>
      <p data-testid="model-error">{error}</p>
      <ul data-testid="model-list">
        {models.map((m) => (
          <li key={m.ref}>{m.ref}</li>
        ))}
      </ul>
    </div>
  );
}

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

  // Regression for #1930.
  //
  // When no ModelConfigs are deployed, the backend responds 200 OK but with the
  // `data` field omitted entirely (Go json omitempty), e.g.:
  //   { "error": false, "message": "Successfully listed ModelConfigs" }
  // The provider must read that as "zero models", NOT as a fetch failure.
  // Before the fix, the missing `data` was treated as an error and the UI showed
  // "Failed to fetch models".
  it("shows no error when there are no model configs", async () => {
    // The response below has no `data` key on purpose — that is exactly what an
    // empty list looks like on the wire.
    mockGetModelConfigs.mockResolvedValue({ message: "Successfully listed ModelConfigs" });

    render(
      <AgentsProvider>
        <ModelsConsumer />
      </AgentsProvider>,
    );

    // Wait for the fetch to be made, then let its promise + setState calls flush
    // so we assert on the state *after* fetchModels has run (not the initial state,
    // which also happens to be "no error / no models").
    await waitFor(() => expect(mockGetModelConfigs).toHaveBeenCalled());
    await act(async () => {});

    expect(screen.getByTestId("model-error")).toBeEmptyDOMElement();
    expect(screen.getByTestId("model-list").children).toHaveLength(0);
  });

  // The fix must not hide genuine failures: a real backend error should still be
  // surfaced via the provider's `error` state.
  it("surfaces a real error returned by the backend", async () => {
    mockGetModelConfigs.mockResolvedValue({
      message: "Failed",
      error: "model configs unavailable",
    });

    render(
      <AgentsProvider>
        <ModelsConsumer />
      </AgentsProvider>,
    );

    await waitFor(() =>
      expect(screen.getByTestId("model-error")).toHaveTextContent("model configs unavailable"),
    );
  });
});
