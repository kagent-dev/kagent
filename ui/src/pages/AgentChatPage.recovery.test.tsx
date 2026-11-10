import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import { SessionService } from "@/generated/kagent/api/v1/sessions_pb";
import { CheckpointService, CheckpointState } from "@/generated/kagent/api/v1/checkpoints_pb";
import { RuntimeOperation, RuntimeState } from "@/generated/kagent/api/v1/runtime_pb";
import { setApiTransport } from "@/api/transport";
import { ApiError, type ChatController } from "@/api";
import { SEND_NOT_ACCEPTED } from "@/api/chat/a2aGrpcChatClient";
import { themeFor } from "@/theme/theme";
import { AgentChatPage } from "./AgentChatPage";

/*
 * Crash recovery as the chat page shows it: read from the stored Session, plus an
 * explicit Recover for a recovery that does not finish and for sends the gateway
 * keeps refusing. Recover calls RevertSession, so it works whether or not the
 * controller recovers on send.
 */

// Only the A2A chat controller is faked; Session reads go through the real hooks and client.
const chat = {
  messages: [],
  isLoadingHistory: false,
  turnState: "idle",
  turnPhase: "idle",
  phase: "idle",
  refreshTranscript: vi.fn(() => Promise.resolve()),
  send: vi.fn(),
  cancel: vi.fn(),
  dismissQuestion: vi.fn(),
  answerQuestion: vi.fn(),
  answerToolApproval: vi.fn(),
  retry: vi.fn(),
} as unknown as ChatController;

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useChat: () => chat,
}));

const ID = "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44";
const LIFECYCLE_POLL_MS = 1_000;

type Lifecycle = {
  state: RuntimeState;
  operation: RuntimeOperation;
  failure?: { reason: string; message: string };
};

const READY: Lifecycle = { state: RuntimeState.READY, operation: RuntimeOperation.NONE };
const RECOVERING: Lifecycle = {
  state: RuntimeState.FAILED,
  operation: RuntimeOperation.REVERT,
  failure: { reason: "RuntimeCrashed", message: "runtime crashed" },
};
const LOST: Lifecycle = {
  state: RuntimeState.FAILED,
  operation: RuntimeOperation.NONE,
  failure: { reason: "RuntimeLost", message: "The Session runtime no longer exists." },
};

let lifecycle: Lifecycle;
let checkpointState: CheckpointState | undefined;
let getSession: ReturnType<typeof vi.fn>;
let revertSession: ReturnType<typeof vi.fn>;
/** What a revert leaves stored; READY unless a test says otherwise. */
let afterRevert: Lifecycle;

function session() {
  return {
    id: ID,
    contextId: ID,
    creator: "alice@example.com",
    agent: { ref: { namespace: "kagent", name: "assistant" }, uid: "agent-uid" },
    ...lifecycle,
  };
}

function renderPage() {
  setApiTransport(createRouterTransport(({ service }) => {
    service(SessionService, {
      getSession: (getSession = vi.fn(() => ({ session: session() }))),
      revertSession: (revertSession = vi.fn(() => {
        lifecycle = afterRevert;
        return { session: session() };
      })),
      listSessions: () => ({ sessions: [session()] }),
    });
    service(CheckpointService, {
      listCheckpoints: () => ({
        checkpoints: checkpointState === undefined
          ? []
          : [{ id: "checkpoint-1", sessionId: ID, headTaskId: "task-1", state: checkpointState }],
      }),
    });
  }));
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false, revalidateOnFocus: false }}>
        <MemoryRouter initialEntries={[`/agents/${ID}/chat`]}>
          <Routes>
            <Route path="/agents/:id/chat" element={<AgentChatPage />} />
          </Routes>
        </MemoryRouter>
      </SWRConfig>
    </ThemeProvider>,
  );
}

const composer = () => screen.getByTestId("chat-input");

async function tick(ms: number) {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
}

beforeEach(() => {
  // Only intervals are faked: the page's polls are intervals, and SWR and the
  // transport keep their real timeouts and promises.
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  checkpointState = undefined;
  afterRevert = READY;
  (chat as { turnError?: Error }).turnError = undefined;
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
  setApiTransport(undefined);
});

describe("a crash recovery in progress", () => {
  it("says the conversation is recovering and enables the composer once it is ready", async () => {
    lifecycle = RECOVERING;
    renderPage();
    expect(await screen.findByText("Recovering this conversation after a crash")).toBeInTheDocument();
    expect(composer()).toBeDisabled();

    // The stored revert operation keeps the lifecycle poll running.
    lifecycle = READY;
    const reads = getSession.mock.calls.length;
    await tick(LIFECYCLE_POLL_MS);
    await waitFor(() => expect(getSession.mock.calls.length).toBeGreaterThan(reads));

    await waitFor(() => expect(composer()).toBeEnabled());
    expect(screen.queryByTestId("chat-not-ready")).not.toBeInTheDocument();
  });
});

describe("a crash recovery that does not finish", () => {
  it("offers Recover, which reverts the Session and enables the composer", async () => {
    lifecycle = RECOVERING;
    renderPage();
    const alert = await screen.findByTestId("chat-not-ready");
    fireEvent.click(within(alert).getByRole("button", { name: "Recover" }));
    await waitFor(() => expect(revertSession).toHaveBeenCalledTimes(1));
    expect(revertSession.mock.calls[0][0]).toMatchObject({ sessionId: ID });
    await waitFor(() => expect(composer()).toBeEnabled());
  });
});

describe("a send the gateway never accepted", () => {
  const refused = () => new ApiError("The agent's runtime did not accept the message. It may have crashed.", {
    kind: "http", code: "FailedPrecondition", reason: SEND_NOT_ACCEPTED, url: "A2AService/SendStreamingMessage",
  });

  it("says the runtime may have crashed and offers Recover", async () => {
    lifecycle = READY;
    (chat as { turnError?: Error }).turnError = refused();
    renderPage();
    const alert = await screen.findByTestId("chat-turn-error");
    expect(alert).toHaveTextContent("The agent's runtime may have crashed");
    fireEvent.click(within(alert).getByRole("button", { name: "Recover" }));
    await waitFor(() => expect(revertSession).toHaveBeenCalledTimes(1));
  });

  it("shows the recorded loss when Recover finds the runtime gone", async () => {
    lifecycle = READY;
    afterRevert = LOST;
    (chat as { turnError?: Error }).turnError = refused();
    renderPage();
    fireEvent.click(within(await screen.findByTestId("chat-turn-error")).getByRole("button", { name: "Recover" }));
    expect(await screen.findByText("This conversation's runtime cannot be recovered")).toBeInTheDocument();
  });

  it("offers only Retry for any other failure", async () => {
    lifecycle = READY;
    (chat as { turnError?: Error }).turnError = new Error("model unavailable");
    renderPage();
    const alert = await screen.findByTestId("chat-turn-error");
    expect(within(alert).queryByRole("button", { name: "Recover" })).not.toBeInTheDocument();
    expect(within(alert).getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

describe("a runtime that cannot be recovered", () => {
  it("says so, keeps the composer disabled, and does not poll a state that never changes", async () => {
    lifecycle = LOST;
    renderPage();
    const alert = await screen.findByTestId("chat-not-ready");
    expect(alert).toHaveTextContent("This conversation's runtime cannot be recovered");
    expect(alert).toHaveTextContent("The Session runtime no longer exists.");
    expect(alert).toHaveTextContent("Start another conversation. This one can still be read.");
    expect(composer()).toBeDisabled();

    const reads = getSession.mock.calls.length;
    await tick(LIFECYCLE_POLL_MS * 3);
    expect(getSession.mock.calls.length).toBe(reads);
  });

  it("offers forking only when a saved snapshot can fork", async () => {
    lifecycle = LOST;
    checkpointState = CheckpointState.READY;
    renderPage();
    await waitFor(() =>
      expect(screen.getByTestId("chat-not-ready")).toHaveTextContent("fork a saved snapshot from the transcript"),
    );
  });
});
