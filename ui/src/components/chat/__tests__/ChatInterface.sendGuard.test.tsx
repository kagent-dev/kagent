/**
 * @jest-environment jsdom
 */
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Message, Task, TaskStatusUpdateEvent } from "@a2a-js/sdk";
import { createSession, getSessionTasks, getSessionWithEvents } from "@/app/actions/sessions";
import { kagentA2AClient } from "@/lib/a2aClient";
import { toast } from "sonner";
import ChatInterface from "@/components/chat/ChatInterface";
import type { Session } from "@/types";

jest.mock("@/app/actions/sessions", () => ({
  createSession: jest.fn(),
  getSessionTasks: jest.fn(),
  getSessionWithEvents: jest.fn(),
}));

jest.mock("@/app/actions/agents", () => ({
  getAgentWithResolvedKind: jest.fn(),
  waitForSandboxAgentReady: jest.fn(),
}));

jest.mock("@/lib/a2aClient", () => ({
  kagentA2AClient: {
    sendMessageStream: jest.fn(),
    resubscribeStream: jest.fn(),
  },
}));

jest.mock("sonner", () => ({
  toast: {
    info: jest.fn(),
    error: jest.fn(),
    loading: jest.fn(),
    dismiss: jest.fn(),
  },
}));

jest.mock("@/hooks/useSpeechRecognition", () => ({
  useSpeechRecognition: () => ({
    isListening: false,
    isSupported: false,
    startListening: jest.fn(),
    stopListening: jest.fn(),
    error: null,
  }),
}));

jest.mock("@/components/chat/ChatAgentContext", () => ({
  useChatRunInSandbox: () => false,
  useChatSubstrateSandbox: () => false,
}));

jest.mock("@/components/chat/ChatMessage", () => ({
  __esModule: true,
  default: ({ message }: { message: Message }) => (
    <div data-testid={`chat-message-${message.role}`}>
      {message.parts
        ?.map((part) => part.kind === "text" ? part.text : JSON.stringify(part))
        .join("")}
    </div>
  ),
}));

jest.mock("@/components/chat/StreamingMessage", () => ({
  __esModule: true,
  default: ({ content }: { content: string }) => <div>{content}</div>,
}));

jest.mock("@/components/chat/ShareButton", () => ({
  __esModule: true,
  default: () => null,
}));

const mockCreateSession = createSession as jest.MockedFunction<typeof createSession>;
const mockGetSessionTasks = getSessionTasks as jest.MockedFunction<typeof getSessionTasks>;
const mockGetSessionWithEvents = getSessionWithEvents as jest.MockedFunction<typeof getSessionWithEvents>;
const mockSendMessageStream = kagentA2AClient.sendMessageStream as jest.MockedFunction<typeof kagentA2AClient.sendMessageStream>;
const mockToastInfo = toast.info as jest.MockedFunction<typeof toast.info>;

const staleToastMessage = "New messages loaded — please review before sending";

// The send guard is server-authoritative: it compares the count of persisted
// history messages across all tasks (the high-water mark) against the count this
// tab last synced. These helpers build tasks whose `history.length` drives that
// count — the message content is irrelevant to the guard.

// The backend snapshot the mocked getSessionTasks currently returns. The stream
// generators advance it to model a turn being persisted after it streams.
let currentTasks: Task[] = [];

function textMessage(messageId: string, role: "user" | "agent", text: string, contextId = "session-1", taskId = "task-1"): Message {
  return {
    kind: "message",
    messageId,
    role,
    contextId,
    taskId,
    parts: [{ kind: "text", text }],
    metadata: { timestamp: Date.now() },
  } as Message;
}

/** A completed task whose history (a user + agent turn) contributes 2 to the mark. */
function completedTurnTask(taskId: string, prompt: string, answer: string, contextId = "session-1"): Task {
  return {
    id: taskId,
    contextId,
    status: {
      state: "completed",
      timestamp: new Date().toISOString(),
    },
    history: [
      textMessage(`${taskId}-user`, "user", prompt, contextId, taskId),
      textMessage(`${taskId}-agent`, "agent", answer, contextId, taskId),
    ],
  } as Task;
}

function completedStatusEvent(text: string, contextId = "session-1", taskId = "task-streamed"): TaskStatusUpdateEvent {
  return {
    kind: "status-update",
    contextId,
    taskId,
    final: true,
    status: {
      state: "completed",
      timestamp: new Date().toISOString(),
      message: textMessage(`assistant-${taskId}`, "agent", text, contextId, taskId),
    },
  } as TaskStatusUpdateEvent;
}

/** Yields the given events, then advances the backend snapshot as if the turn was persisted. */
async function* streamThenPersist(events: unknown[], persistedTasks: Task[]): AsyncIterable<unknown> {
  for (const event of events) {
    yield event;
  }
  currentTasks = persistedTasks;
}

function sessionFixture(overrides: Partial<Session> = {}): Session {
  return {
    id: "session-1",
    name: "Existing chat",
    agent_id: "kagent__NS__test-agent",
    user_id: "user-1",
    created_at: "2026-03-07T10:00:00Z",
    updated_at: "2026-03-07T10:05:00Z",
    deleted_at: "",
    ...overrides,
  };
}

function renderExistingSession() {
  return render(
    <ChatInterface
      selectedAgentName="test-agent"
      selectedNamespace="kagent"
      sessionId="session-1"
      selectedSession={sessionFixture()}
    />,
  );
}

async function sendText(text: string) {
  const user = userEvent.setup();
  const textbox = screen.getByRole("textbox");
  await waitFor(() => expect(textbox).not.toBeDisabled());
  await user.clear(textbox);
  await user.type(textbox, text);
  await user.click(screen.getByRole("button", { name: /send/i }));
}

describe("ChatInterface send guard (high-water mark)", () => {
  const initialTasks = () => [completedTurnTask("task-initial", "initial user", "initial answer")];

  beforeEach(() => {
    jest.clearAllMocks();
    mockCreateSession.mockResolvedValue({ error: "unexpected createSession call" });
    mockGetSessionWithEvents.mockResolvedValue({ data: { session: sessionFixture(), events: [], read_only: false } });
    // Every getSessionTasks (load, guard, refreshServerMark, reload) reads the
    // current backend snapshot; streams mutate it to simulate persistence.
    mockGetSessionTasks.mockImplementation(async () => ({ data: currentTasks }));
  });

  it("does not block the next send after a same-tab turn advances the mark", async () => {
    currentTasks = initialTasks();
    const afterFirstTurn = [...initialTasks(), completedTurnTask("task-streamed", "same tab question", "same tab answer")];
    mockSendMessageStream
      .mockResolvedValueOnce(streamThenPersist([completedStatusEvent("same tab answer")], afterFirstTurn))
      .mockResolvedValueOnce(streamThenPersist([completedStatusEvent("next answer", "session-1", "task-next")], afterFirstTurn));

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();
    // Load synced the mark to the initial history count.
    await waitFor(() => expect(mockGetSessionTasks).toHaveBeenCalledTimes(1));

    await sendText("same tab question");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("same tab answer")).toBeInTheDocument();
    // Wait until refreshServerMark has re-read the post-turn snapshot (load +
    // guard + refresh = 3 reads) so the mark reflects our own new messages.
    await waitFor(() => expect(mockGetSessionTasks).toHaveBeenCalledTimes(3));

    await sendText("next question");

    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(2));
    expect(mockToastInfo).not.toHaveBeenCalledWith(staleToastMessage);
  });

  it("blocks the send when another tab advanced the conversation past the synced mark", async () => {
    currentTasks = initialTasks();

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();
    await waitFor(() => expect(mockGetSessionTasks).toHaveBeenCalledTimes(1));

    // Another tab added a turn the server persisted but this tab never synced.
    currentTasks = [...initialTasks(), completedTurnTask("task-external", "external user", "external answer")];

    await sendText("should review cross-tab first");

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).not.toHaveBeenCalled();
    // The block reloaded the latest context for the user.
    expect(await screen.findByText("external answer")).toBeInTheDocument();
  });

  it("proceeds after a block once the reload re-syncs the mark", async () => {
    currentTasks = initialTasks();
    mockSendMessageStream.mockResolvedValueOnce(streamThenPersist([completedStatusEvent("ok")], currentTasks));

    renderExistingSession();
    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    // First send is stale and blocked; reloadSessionFromDB advances the mark.
    currentTasks = [...initialTasks(), completedTurnTask("task-external", "external user", "external answer")];
    await sendText("first try");
    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).not.toHaveBeenCalled();

    // Nothing else changed on the server, so the next send goes through.
    await sendText("second try");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));
  });

  it.each([
    ["Cmd+Enter", { metaKey: true }],
    ["Ctrl+Enter", { ctrlKey: true }],
  ])("applies the stale-message send guard for %s", async (_shortcut, modifier) => {
    const user = userEvent.setup();
    currentTasks = initialTasks();

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();
    await waitFor(() => expect(mockGetSessionTasks).toHaveBeenCalledTimes(1));

    currentTasks = [...initialTasks(), completedTurnTask("task-external", "external user", "external answer")];

    const textbox = screen.getByRole("textbox");
    await user.type(textbox, "should review first");
    fireEvent.keyDown(textbox, { key: "Enter", code: "Enter", ...modifier });

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).not.toHaveBeenCalled();
  });
});
