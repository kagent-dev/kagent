import { act, render, screen } from "@testing-library/react";
import { ExecutionHistoryTable } from "@/components/schedules/ExecutionHistoryTable";

describe("ExecutionHistoryTable", () => {
  afterEach(() => {
    jest.useRealTimers();
  });

  it("shows a live duration for an in-progress execution", () => {
    jest.useFakeTimers();
    jest.setSystemTime(new Date("2026-07-22T10:01:05Z"));

    render(
      <ExecutionHistoryTable
        executions={[{
          id: "running-execution",
          startTime: "2026-07-22T10:00:00Z",
          trigger: "Scheduled",
          sessionID: "running-session",
          status: "InProgress",
        }]}
      />,
    );

    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("1m 5s")).toBeInTheDocument();

    act(() => {
      jest.advanceTimersByTime(1000);
    });
    expect(screen.getByText("1m 6s")).toBeInTheDocument();
  });

  it("links a scheduled session to the target agent chat", () => {
    render(
      <ExecutionHistoryTable
        executions={[{
          id: "completed-execution",
          startTime: "2026-07-22T10:00:00Z",
          completionTime: "2026-07-22T10:00:03Z",
          trigger: "Manual",
          sessionID: "session/with spaces",
          status: "Succeeded",
        }]}
        targetNamespace="team-a"
        targetName="report agent"
      />,
    );

    expect(screen.getByRole("link", { name: "Open session session/with spaces" })).toHaveAttribute(
      "href",
      "/agents/team-a/report%20agent/chat/session%2Fwith%20spaces",
    );
    expect(screen.getByText("3s")).toBeInTheDocument();
    expect(screen.getByText("Manual")).toBeInTheDocument();
  });
});
