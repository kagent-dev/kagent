/**
 * @jest-environment jsdom
 */
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { deleteAgent } from "@/app/actions/agents";
import { DeleteButton } from "@/components/DeleteAgentButton";

jest.mock("@/app/actions/agents", () => ({
  deleteAgent: jest.fn(),
}));

const mockDeleteAgent = deleteAgent as jest.MockedFunction<typeof deleteAgent>;

describe("DeleteButton", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("invokes onDeleted after a successful delete", async () => {
    const user = userEvent.setup();
    const onDeleted = jest.fn();
    mockDeleteAgent.mockResolvedValue({ message: "Successfully deleted agent" });

    render(
      <DeleteButton
        agentName="test-agent"
        namespace="kagent"
        externalOpen={true}
        onExternalOpenChange={jest.fn()}
        onDeleted={onDeleted}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mockDeleteAgent).toHaveBeenCalledWith("test-agent", "kagent");
    });
    await waitFor(() => expect(onDeleted).toHaveBeenCalledTimes(1));
  });

  it("does not invoke onDeleted when deleteAgent returns an error response", async () => {
    const user = userEvent.setup();
    const onDeleted = jest.fn();
    const consoleError = jest.spyOn(console, "error").mockImplementation(() => {});
    mockDeleteAgent.mockResolvedValue({ message: "boom", error: "boom" });

    render(
      <DeleteButton
        agentName="test-agent"
        namespace="kagent"
        externalOpen={true}
        onExternalOpenChange={jest.fn()}
        onDeleted={onDeleted}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mockDeleteAgent).toHaveBeenCalledWith("test-agent", "kagent");
    });
    await waitFor(() => expect(consoleError).toHaveBeenCalled());
    expect(onDeleted).not.toHaveBeenCalled();
  });
});
