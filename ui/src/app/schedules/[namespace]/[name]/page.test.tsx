import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useParams, useRouter } from "next/navigation";
import ScheduledRunDetailPage from "@/app/schedules/[namespace]/[name]/page";
import {
  deleteScheduledRun,
  getScheduledRun,
  getScheduledRunExecutions,
} from "@/app/actions/scheduledRuns";
import type { ScheduledRun } from "@/types";

jest.mock("next/navigation", () => ({
  useParams: jest.fn(),
  useRouter: jest.fn(),
}));

jest.mock("@/app/actions/scheduledRuns", () => ({
  deleteScheduledRun: jest.fn(),
  getScheduledRun: jest.fn(),
  getScheduledRunExecutions: jest.fn(),
  triggerScheduledRun: jest.fn(),
  updateScheduledRun: jest.fn(),
}));

jest.mock("@/components/schedules/ExecutionHistoryTable", () => ({
  ExecutionHistoryTable: () => <div>Execution history</div>,
}));

jest.mock("sonner", () => ({
  toast: {
    error: jest.fn(),
    success: jest.fn(),
  },
}));

const mockUseParams = useParams as jest.Mock;
const mockUseRouter = useRouter as jest.Mock;
const mockGetScheduledRun = getScheduledRun as jest.MockedFunction<typeof getScheduledRun>;
const mockGetScheduledRunExecutions = getScheduledRunExecutions as jest.MockedFunction<
  typeof getScheduledRunExecutions
>;
const mockDeleteScheduledRun = deleteScheduledRun as jest.MockedFunction<
  typeof deleteScheduledRun
>;

const scheduledRun: ScheduledRun = {
  metadata: {
    name: "nightly",
    namespace: "kagent",
  },
  spec: {
    schedule: "0 0 * * *",
    targetRef: {
      apiGroup: "kagent.dev",
      kind: "Agent",
      name: "assistant",
    },
    prompt: "Summarize activity",
  },
};

describe("ScheduledRunDetailPage", () => {
  const push = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
    mockUseParams.mockReturnValue({ namespace: "kagent", name: "nightly" });
    mockUseRouter.mockReturnValue({ push });
    mockGetScheduledRun.mockResolvedValue({ message: "ok", data: scheduledRun });
    mockGetScheduledRunExecutions.mockResolvedValue({ message: "ok", data: [] });
  });

  it("surfaces execution-history request errors", async () => {
    mockGetScheduledRunExecutions.mockResolvedValue({
      message: "failed",
      error: "Execution history unavailable",
    });

    render(<ScheduledRunDetailPage />);

    expect(await screen.findByText("Execution history unavailable")).toBeInTheDocument();
  });

  it("disables delete actions and shows progress while deletion is pending", async () => {
    const user = userEvent.setup();
    let finishDelete: ((value: { message: string }) => void) | undefined;
    mockDeleteScheduledRun.mockReturnValue(new Promise((resolve) => {
      finishDelete = resolve;
    }));

    render(<ScheduledRunDetailPage />);
    await screen.findByText("Execution history");
    await user.click(screen.getByRole("button", { name: "Delete" }));

    const dialog = await screen.findByRole("dialog");
    const confirmButton = within(dialog).getByRole("button", { name: "Delete" });
    await user.click(confirmButton);

    expect(within(dialog).getByRole("button", { name: "Deleting..." })).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeDisabled();

    await act(async () => {
      finishDelete?.({ message: "deleted" });
    });
    expect(push).toHaveBeenCalledWith("/schedules");
  });
});
