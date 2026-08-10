/**
 * @jest-environment jsdom
 */
import { act, render, screen, waitFor } from "@testing-library/react";
import { Role, type Task } from "@a2a-js/sdk";
import { checkSessionExists, createSession, getSessionTasks } from "@/app/actions/sessions";
import { kagentA2AClient } from "@/lib/a2aClient";
import ChatInterface from "@/components/chat/ChatInterface";
import { createMockTask, createMockTextMessage, createTextPart } from "@/mocks/factories";
import type { BaseResponse } from "@/types";

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
  toast: { info: jest.fn(), error: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
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
  useCurrentChatAgent: () => ({ deploymentReady: true }),
}));

jest.mock("@/components/chat/ChatMessage", () => ({
  __esModule: true,
  default: ({ message }: { message: import("@a2a-js/sdk").Message }) => (
    <div data-testid={`chat-message-${message.role}`}>
      {message.parts
        ?.map((part) => (part.content?.$case === "text" ? part.content.value : ""))
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
const mockResubscribeStream = kagentA2AClient.resubscribeStream as jest.MockedFunction<
  typeof kagentA2AClient.resubscribeStream
>;

/** A completed A2A v1 turn: user request in history, agent output in an artifact. */
function task(sessionId: string, answer: string): Task {
  const taskId = `task-${sessionId}`;
  const result = createMockTask(taskId, sessionId, [
    createMockTextMessage(`${sessionId}-user`, Role.ROLE_USER, "hi", {
      contextId: sessionId,
      taskId,
    }),
  ]);
  result.artifacts = [{
    artifactId: `${taskId}-answer`,
    name: "",
    description: "",
    parts: [createTextPart(answer)],
    extensions: [],
    metadata: undefined,
  }];
  return result;
}

/** A promise this test can resolve on demand, to control arrival order. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe("ChatInterface session switch", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockCheckSessionExists.mockResolvedValue({ message: "ok", data: true });
    mockCreateSession.mockResolvedValue({ message: "unexpected createSession call", error: "unexpected createSession call" });
    mockResubscribeStream.mockReturnValue(Promise.resolve((async function* () {})()));
  });

  it("does not show a stale session's messages after they arrive out of order", async () => {
    const sessionA = deferred<BaseResponse<Task[]>>();
    const sessionB = deferred<BaseResponse<Task[]>>();
    mockGetSessionTasks.mockImplementation(async (sessionId: string) =>
      sessionId === "session-a" ? sessionA.promise : sessionB.promise,
    );

    const { rerender } = render(<ChatInterface selectedAgentName="test-agent" selectedNamespace="kagent" sessionId="session-a" />);
    await waitFor(() => expect(mockGetSessionTasks).toHaveBeenCalledWith("session-a", undefined));

    rerender(<ChatInterface selectedAgentName="test-agent" selectedNamespace="kagent" sessionId="session-b" />);
    await waitFor(() => expect(mockGetSessionTasks).toHaveBeenCalledWith("session-b", undefined));

    // session-b's fetch resolves first, then session-a's late response arrives.
    sessionB.resolve({ message: "ok", data: [task("session-b", "answer b")] });
    await screen.findByText("answer b");

    // Let the late session-a response run its full async continuation past
    // the awaited getSessionTasks call before asserting on the DOM.
    await act(async () => {
      sessionA.resolve({ message: "ok", data: [task("session-a", "answer a")] });
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(screen.queryByText("answer a")).not.toBeInTheDocument();
    expect(screen.getByText("answer b")).toBeInTheDocument();
  });
});
