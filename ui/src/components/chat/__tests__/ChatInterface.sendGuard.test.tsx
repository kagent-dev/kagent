/**
 * @jest-environment jsdom
 */
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Role, TaskState, type Message, type StreamResponse, type Task } from "@a2a-js/sdk";
import { checkSessionExists, createSession, getSessionTasks } from "@/app/actions/sessions";
import { kagentA2AClient } from "@/lib/a2aClient";
import { toast } from "sonner";
import ChatInterface from "@/components/chat/ChatInterface";
import { createMockSession, createMockTask, createMockTextMessage, createTextPart } from "@/mocks/factories";
import type { Session } from "@/types";

jest.mock("@/app/actions/sessions", () => ({
  checkSessionExists: jest.fn(),
  createSession: jest.fn(),
  getSessionTasks: jest.fn(),
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
  useChatSubstrateSandbox: () => true,
  useCurrentChatAgent: () => ({ ready: true }),
}));

jest.mock("@/components/chat/ChatMessage", () => ({
  __esModule: true,
  default: ({ message }: { message: Message }) => (
    <div data-testid={`chat-message-${message.role}`}>
      {message.parts
        ?.map((part) => {
          if ((part as { content?: { $case?: string; value?: unknown } }).content?.$case === "text") {
            return (part as { content?: { value?: string } }).content?.value ?? "";
          }
          return JSON.stringify(part);
        })
        .join("")}
    </div>
  ),
}));

jest.mock("@/components/chat/ShareButton", () => ({
  __esModule: true,
  default: () => null,
}));

jest.mock("@/components/chat/StreamingMessage", () => ({
  __esModule: true,
  default: ({ content }: { content: string }) => <div>{content}</div>,
}));

const mockCheckSessionExists = checkSessionExists as jest.MockedFunction<typeof checkSessionExists>;
const mockCreateSession = createSession as jest.MockedFunction<typeof createSession>;
const mockGetSessionTasks = getSessionTasks as jest.MockedFunction<typeof getSessionTasks>;
const mockSendMessageStream = kagentA2AClient.sendMessageStream as jest.MockedFunction<typeof kagentA2AClient.sendMessageStream>;
const mockToastInfo = toast.info as jest.MockedFunction<typeof toast.info>;

const staleToastMessage = "New messages loaded — please review before sending";

// The send guard is server-authoritative: it compares the persisted user-message
// high-water mark against what this tab last synced. In A2A v1, assistant output
// belongs in artifacts; history contains the user request that created the task.

// The backend snapshot the mocked getSessionTasks currently returns. The stream
// generators advance it to model a turn being persisted after it streams.
let currentTasks: Task[] = [];

function textMessage(messageId: string, role: Role, text: string, contextId = "session-1", taskId = "task-1"): Message {
  return createMockTextMessage(messageId, role, text, {
    contextId,
    taskId,
    metadata: { timestamp: Date.now() },
  });
}

/** A completed A2A v1 turn: user input in history and agent output in an artifact. */
function completedTurnTask(taskId: string, prompt: string, answer: string, contextId = "session-1"): Task {
  const task = createMockTask(taskId, contextId, [
    textMessage(`${taskId}-user`, Role.ROLE_USER, prompt, contextId, taskId),
  ]);
  task.artifacts = [{
    artifactId: `${taskId}-answer`,
    name: "",
    description: "",
    parts: [createTextPart(answer)],
    extensions: [],
    metadata: undefined,
  }];
  return task;
}

function finalArtifactEvent(text: string, contextId = "session-1", taskId = "task-streamed"): StreamResponse {
  return {
    payload: {
      $case: "artifactUpdate",
      value: {
        contextId,
        taskId,
        metadata: undefined,
        artifact: {
          artifactId: `${taskId}-answer`,
          name: "",
          description: "",
          parts: [createTextPart(text)],
          extensions: [],
          metadata: undefined,
        },
        append: false,
        lastChunk: true,
      },
    },
  } as StreamResponse;
}

function completedStatusEvent(contextId = "session-1", taskId = "task-streamed"): StreamResponse {
  return {
    payload: {
      $case: "statusUpdate",
      value: {
        contextId,
        taskId,
        metadata: undefined,
        status: {
          state: TaskState.TASK_STATE_COMPLETED,
          timestamp: new Date().toISOString(),
          message: undefined,
        },
      },
    },
  } as StreamResponse;
}

/** Yields the given events, then advances the backend snapshot as if the turn was persisted. */
async function* streamThenPersist(events: StreamResponse[], persistedTasks: Task[]): AsyncIterable<StreamResponse> {
  for (const event of events) {
    yield event;
  }
  currentTasks = persistedTasks;
}

function sessionFixture(overrides: Partial<Session> = {}): Session {
  return createMockSession({
    id: "session-1",
    name: "Existing chat",
    agent_id: "kagent__NS__test-agent",
    user_id: "user-1",
    ...overrides,
  });
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
    mockCheckSessionExists.mockResolvedValue({ message: "ok", data: true });
    mockCreateSession.mockResolvedValue({ message: "unexpected createSession call", error: "unexpected createSession call" });
    // Every getSessionTasks (load, guard, refreshServerMark, reload) reads the
    // current backend snapshot; streams mutate it to simulate persistence.
    mockGetSessionTasks.mockImplementation(async () => ({ message: "ok", data: currentTasks }));
  });

  it("does not block the next send after a same-tab turn advances the mark", async () => {
    currentTasks = initialTasks();
    const afterFirstTurn = [...initialTasks(), completedTurnTask("task-streamed", "same tab question", "same tab answer")];
    mockSendMessageStream
      .mockResolvedValueOnce(streamThenPersist([
        finalArtifactEvent("same tab answer"),
        completedStatusEvent(),
      ], afterFirstTurn))
      .mockResolvedValueOnce(streamThenPersist([
        finalArtifactEvent("next answer", "session-1", "task-next"),
        completedStatusEvent("session-1", "task-next"),
      ], afterFirstTurn));

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
    mockSendMessageStream.mockResolvedValueOnce(streamThenPersist([
      finalArtifactEvent("ok"),
      completedStatusEvent(),
    ], currentTasks));

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
