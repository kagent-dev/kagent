import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import toast from "react-hot-toast";
import { SessionService } from "@/generated/kagent/api/v1/sessions_pb";
import { CheckpointService } from "@/generated/kagent/api/v1/checkpoints_pb";
import { RuntimeOperation, RuntimeState } from "@/generated/kagent/api/v1/runtime_pb";
import { setApiTransport } from "@/api/transport";
import type { ChatController } from "@/api";
import { themeFor } from "@/theme/theme";
import { AgentChatPage } from "./AgentChatPage";

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
const LIVE_REFRESH_MS = 4_000;


let lifecycle: { state: RuntimeState; operation: RuntimeOperation };

function session(recoveryNeeded?: boolean) {
  return {
    id: ID,
    contextId: ID,
    creator: "alice@example.com",
    agent: { ref: { namespace: "kagent", name: "assistant" }, uid: "agent-uid" },
    ...lifecycle,
    recoveryNeeded,
  };
}

const HEALTHY = false;
const CRASHED = true;
const IDLE = { state: RuntimeState.READY, operation: RuntimeOperation.NONE };
/** An earlier attempt admitted the recovery, then failed; it stays pending until a retry. */
const ADMITTED = { state: RuntimeState.FAILED, operation: RuntimeOperation.REVERT };

let backend: boolean;
let getSession: ReturnType<typeof vi.fn>;
let revertSession: ReturnType<typeof vi.fn>;
function recovered() {
  backend = HEALTHY;
  lifecycle = IDLE;
  return { session: session(backend) };
}

type Reverted = ReturnType<typeof recovered>;
let revert: () => Reverted | Promise<Reverted>;

function renderPage() {
  setApiTransport(createRouterTransport(({ service }) => {
    service(SessionService, {
      getSession: (getSession = vi.fn(() => ({ session: session(backend) }))),
      listSessions: () => ({ sessions: [session(backend)] }),
      revertSession: (revertSession = vi.fn(() => revert())),
    });
    service(CheckpointService, { listCheckpoints: () => ({ checkpoints: [] }) });
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
  vi.mocked(chat.refreshTranscript).mockClear();
  lifecycle = IDLE;
  revert = recovered;
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
  setApiTransport(undefined);
});

describe("a crash found on an open conversation", () => {
  it("finds a crash that happens after the page went idle", async () => {
    backend = HEALTHY;
    renderPage();
    await waitFor(() => expect(composer()).toBeEnabled());
    expect(screen.queryByText("The agent runtime crashed")).not.toBeInTheDocument();

    // The Actor crashes with no turn running, so nothing on the page is polling closely.
    backend = CRASHED;
    await tick(LIVE_REFRESH_MS);

    expect(await screen.findByText("The agent runtime crashed")).toBeInTheDocument();
    expect(composer()).toBeDisabled();
  });

  it("does not re-read a crashed Session every second while it waits on the reader", async () => {
    backend = CRASHED;
    renderPage();
    await screen.findByText("The agent runtime crashed");
    await waitFor(() => expect(composer()).toBeDisabled());

    const before = getSession.mock.calls.length;
    await tick(LIVE_REFRESH_MS - 1);
    expect(getSession.mock.calls.length).toBe(before);
    await tick(1);
    await waitFor(() => expect(getSession.mock.calls.length).toBe(before + 1));
  });

  it("reverts, then re-reads the Session and the transcript and enables the composer", async () => {
    backend = CRASHED;
    renderPage();
    await screen.findByText("The agent runtime crashed");
    expect(composer()).toBeDisabled();
    const reads = getSession.mock.calls.length;

    await userEvent.click(screen.getByRole("button", { name: "Revert to last snapshot" }));

    await waitFor(() => expect(composer()).toBeEnabled());
    expect(revertSession).toHaveBeenCalledOnce();
    expect(getSession.mock.calls.length).toBeGreaterThan(reads);
    expect(chat.refreshTranscript).toHaveBeenCalled();
    expect(screen.queryByText("The agent runtime crashed")).not.toBeInTheDocument();
  });
});

describe("a recovery admitted by a failed attempt", () => {
  const revertButton = () => screen.getByRole("button", { name: /Revert to last snapshot/ });

  it("keeps Revert usable, and a click joins the admitted recovery", async () => {
    backend = CRASHED;
    lifecycle = ADMITTED;
    renderPage();
    await screen.findByText("Recovering this conversation");
    await waitFor(() => expect(revertButton()).toBeEnabled());

    await userEvent.click(revertButton());

    await waitFor(() => expect(screen.queryByText("Recovering this conversation")).not.toBeInTheDocument());
    expect(revertSession).toHaveBeenCalledOnce();
    expect(chat.refreshTranscript).toHaveBeenCalled();
  });

  it("leaves Revert usable when other work refuses it", async () => {
    const error = vi.spyOn(toast, "error");
    let refuse!: () => void;
    revert = () => new Promise<Reverted>((_, reject) => {
      refuse = () => reject(new ConnectError("lifecycle operation is pending", Code.Aborted));
    });
    backend = CRASHED;
    lifecycle = ADMITTED;
    renderPage();
    await screen.findByText("Recovering this conversation");
    await waitFor(() => expect(revertButton()).toBeEnabled());

    act(() => revertButton().click());
    await waitFor(() => expect(revertButton()).toBeDisabled());
    expect(revertSession).toHaveBeenCalledOnce();

    await act(async () => refuse());

    await waitFor(() => expect(revertButton()).toBeEnabled());
    expect(error).toHaveBeenCalledWith(expect.stringContaining("Could not recover the conversation"));
    expect(screen.getByText("Recovering this conversation")).toBeInTheDocument();
  });
});
