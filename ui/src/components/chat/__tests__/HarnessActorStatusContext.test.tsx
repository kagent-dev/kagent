import { act, render, screen } from "@testing-library/react";
import { getAgentHarnessSessionStatus } from "@/app/actions/agentHarnessSession";
import {
  HarnessActorStatusProvider,
  useHarnessActorStatus,
} from "@/components/chat/HarnessActorStatusContext";

jest.mock("@/app/actions/agentHarnessSession", () => ({
  getAgentHarnessSessionStatus: jest.fn(),
}));

const mockGetStatus = getAgentHarnessSessionStatus as jest.MockedFunction<
  typeof getAgentHarnessSessionStatus
>;

function StatusConsumer({ label }: { label: string }) {
  const status = useHarnessActorStatus();
  return <span>{`${label}:${status?.state ?? "loading"}`}</span>;
}

describe("HarnessActorStatusProvider", () => {
  beforeEach(() => {
    jest.useFakeTimers();
    mockGetStatus.mockResolvedValue({ data: { state: "running" } });
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.clearAllMocks();
  });

  it("polls once for all consumers", async () => {
    render(
      <HarnessActorStatusProvider
        namespace="kagent"
        harnessName="harness"
        sessionId="session-1"
        enabled
      >
        <StatusConsumer label="left" />
        <StatusConsumer label="right" />
      </HarnessActorStatusProvider>,
    );

    await act(async () => {
      jest.advanceTimersByTime(0);
      await Promise.resolve();
    });

    expect(mockGetStatus).toHaveBeenCalledTimes(1);
    expect(screen.getByText("left:running")).toBeInTheDocument();
    expect(screen.getByText("right:running")).toBeInTheDocument();

    await act(async () => {
      jest.advanceTimersByTime(12000);
      await Promise.resolve();
    });

    expect(mockGetStatus).toHaveBeenCalledTimes(2);
  });
});
