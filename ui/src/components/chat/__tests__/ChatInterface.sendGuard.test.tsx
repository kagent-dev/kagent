/**
 * @jest-environment jsdom
 */
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataPart, Message, Task, TaskStatusUpdateEvent } from "@a2a-js/sdk";
import { checkSessionExists, createSession, getSessionTasks } from "@/app/actions/sessions";
import { kagentA2AClient } from "@/lib/a2aClient";
import { toast } from "sonner";
import ChatInterface from "@/components/chat/ChatInterface";
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

const mockCheckSessionExists = checkSessionExists as jest.MockedFunction<typeof checkSessionExists>;
const mockCreateSession = createSession as jest.MockedFunction<typeof createSession>;
const mockGetSessionTasks = getSessionTasks as jest.MockedFunction<typeof getSessionTasks>;
const mockSendMessageStream = kagentA2AClient.sendMessageStream as jest.MockedFunction<typeof kagentA2AClient.sendMessageStream>;
const mockToastInfo = toast.info as jest.MockedFunction<typeof toast.info>;

const staleToastMessage = "New messages loaded — please review before sending";

function mockBackendTasks(tasks: Task[]) {
  mockGetSessionTasks.mockResolvedValue({ data: tasks });
}

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

function completedTask(taskId: string, history: Message[], contextId = "session-1"): Task {
  return {
    id: taskId,
    contextId,
    status: {
      state: "completed",
      timestamp: new Date().toISOString(),
    },
    history,
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

function toolCallMessage(messageId: string, contextId = "session-1", taskId = "task-tool", callId = "shared-call"): Message {
  return {
    kind: "message",
    messageId,
    role: "agent",
    contextId,
    taskId,
    parts: [
      {
        kind: "data",
        data: { id: callId, name: "kubectl_get_pods", args: { namespace: "default" } },
        metadata: { adk_type: "function_call" },
      } as DataPart,
    ],
    metadata: { timestamp: Date.now() },
  } as Message;
}

function completedToolCallStatusEvent(contextId = "session-1", taskId = "task-streamed", callId = "shared-call"): TaskStatusUpdateEvent {
  return {
    kind: "status-update",
    contextId,
    taskId,
    final: true,
    status: {
      state: "completed",
      timestamp: new Date().toISOString(),
      message: toolCallMessage(`tool-${taskId}`, contextId, taskId, callId),
    },
  } as TaskStatusUpdateEvent;
}

async function* streamOf(...events: unknown[]): AsyncIterable<unknown> {
  for (const event of events) {
    yield event;
  }
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

function sentMessage(callIndex = 0): Message {
  return (mockSendMessageStream.mock.calls[callIndex][2] as { message: Message }).message;
}

describe("ChatInterface send guard", () => {
  const initialTurn = [
    textMessage("initial-user", "user", "initial user", "session-1", "task-initial"),
    textMessage("initial-agent", "agent", "initial answer", "session-1", "task-initial"),
  ];
  const sameTabTurn = [
    textMessage("same-tab-user", "user", "same tab question", "session-1", "task-streamed"),
    textMessage("same-tab-agent", "agent", "same tab answer", "session-1", "task-streamed"),
  ];
  const externalTurn = [
    textMessage("external-user", "user", "external user", "session-1", "task-external"),
    textMessage("external-agent", "agent", "external answer", "session-1", "task-external"),
  ];

  beforeEach(() => {
    jest.clearAllMocks();
    mockCheckSessionExists.mockResolvedValue({ data: true });
    mockCreateSession.mockResolvedValue({ error: "unexpected createSession call" });
  });

  it("does not block the next send when completed same-tab stream messages are already visible", async () => {
    mockBackendTasks([completedTask("task-initial", initialTurn)]);
    mockSendMessageStream
      .mockResolvedValueOnce(streamOf(completedStatusEvent("same tab answer")))
      .mockResolvedValueOnce(streamOf(completedStatusEvent("next answer", "session-1", "task-next")));

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    await sendText("same tab question");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("same tab answer")).toBeInTheDocument();

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-streamed", [
        textMessage(sentMessage().messageId, "user", "same tab question", "session-1", "task-streamed"),
        textMessage("same-tab-agent", "agent", "same tab answer", "session-1", "task-streamed"),
      ]),
    ]);
    await sendText("next question");

    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(2));
    expect(mockToastInfo).not.toHaveBeenCalledWith(staleToastMessage);
  });

  it("still blocks after a same-tab stream when the backend also has an unseen cross-tab message", async () => {
    mockBackendTasks([completedTask("task-initial", initialTurn)]);
    mockSendMessageStream
      .mockResolvedValueOnce(streamOf(completedStatusEvent("same tab answer")));

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    await sendText("same tab question");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("same tab answer")).toBeInTheDocument();

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-streamed", sameTabTurn),
      completedTask("task-external", externalTurn),
    ]);
    await sendText("should review cross-tab first");

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).toHaveBeenCalledTimes(1);
  });

  it("does not let local-only streaming messages mask an unseen backend turn", async () => {
    mockBackendTasks([completedTask("task-initial", initialTurn)]);
    mockSendMessageStream
      .mockResolvedValueOnce(streamOf(completedStatusEvent("local-only answer")));

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    await sendText("local optimistic question");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("local-only answer")).toBeInTheDocument();

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-external", externalTurn),
    ]);
    await sendText("should review backend first");

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).toHaveBeenCalledTimes(1);
  });

  it("blocks when local-only streaming messages share text and role with an unseen backend turn", async () => {
    mockBackendTasks([completedTask("task-initial", initialTurn)]);
    mockSendMessageStream
      .mockResolvedValueOnce(streamOf(completedStatusEvent("duplicate answer", "session-1", "task-streamed")));

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    await sendText("duplicate question");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("duplicate answer")).toBeInTheDocument();

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-unseen", [
        textMessage("unseen-user", "user", "duplicate question", "session-1", "task-unseen"),
        textMessage("unseen-agent", "agent", "duplicate answer", "session-1", "task-unseen"),
      ]),
    ]);
    await sendText("should review duplicate backend first");

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).toHaveBeenCalledTimes(1);
  });

  it("blocks when a local-only empty tool stream collides with an unseen backend data turn", async () => {
    mockBackendTasks([completedTask("task-initial", initialTurn)]);
    mockSendMessageStream
      .mockResolvedValueOnce(streamOf(completedToolCallStatusEvent("session-1", "task-streamed", "shared-call")));

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    await sendText("local tool question");
    await waitFor(() => expect(mockSendMessageStream).toHaveBeenCalledTimes(1));

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-unseen-tool", [
        toolCallMessage("backend-tool", "session-1", "task-unseen-tool", "shared-call"),
      ]),
    ]);
    await sendText("should review tool turn first");

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).toHaveBeenCalledTimes(1);
  });

  it("still blocks when the backend has a cross-tab message not visible locally", async () => {
    mockBackendTasks([completedTask("task-initial", initialTurn)]);

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-external", externalTurn),
    ]);
    await sendText("should review first");

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).not.toHaveBeenCalled();
  });

  it.each([
    ["Cmd+Enter", { metaKey: true }],
    ["Ctrl+Enter", { ctrlKey: true }],
  ])("applies the stale-message send guard for %s", async (_shortcut, modifier) => {
    const user = userEvent.setup();
    mockBackendTasks([completedTask("task-initial", initialTurn)]);

    renderExistingSession();

    expect(await screen.findByText("initial answer")).toBeInTheDocument();

    mockBackendTasks([
      completedTask("task-initial", initialTurn),
      completedTask("task-external", externalTurn),
    ]);

    const textbox = screen.getByRole("textbox");
    await user.type(textbox, "should review first");
    fireEvent.keyDown(textbox, { key: "Enter", code: "Enter", ...modifier });

    await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(staleToastMessage));
    expect(mockSendMessageStream).not.toHaveBeenCalled();
  });
});
