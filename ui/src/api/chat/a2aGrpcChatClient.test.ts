/**
 * The behaviours the chat client must keep, now that it reads proto rather than JSON.
 *
 * Its predecessor carried a large suite, most of which was about *decoding*: a keyed
 * union with no discriminator, enums spelled as `TASK_STATE_*`, parts with no `kind`,
 * two spellings of `role` in one stream, and frames split across chunk boundaries.
 * None of that exists over gRPC-Web — protobuf-es decodes the wire — so those tests
 * went with the code they guarded.
 *
 * What is tested here is the half that was never about the wire: turning a stream of
 * A2A events into the transcript a reader sees. Every case below was a bug found
 * against a live controller, and none of them is implied by the protocol.
 */

import { afterEach, describe, expect, it } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import type { ConnectRouter } from "@connectrpc/connect";
import { fromJson } from "@bufbuild/protobuf";
import { ValueSchema } from "@bufbuild/protobuf/wkt";
import {
  A2AService,
  Role,
  TaskState,
  type SendMessageRequest,
} from "@/generated/a2a_pb";
import { setApiTransport } from "../transport";
import { A2AGrpcChatClient } from "./a2aGrpcChatClient";
import type { ChatEvent, ChatMessage } from "./types";

const CONVERSATION = {
  namespace: "kagent",
  id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
};

afterEach(() => setApiTransport(undefined));

function serve(routes: (router: ConnectRouter) => void): void {
  setApiTransport(createRouterTransport(routes));
}

/** A text part, in the oneof shape the proto actually uses. */
const text = (value: string) => ({ content: { case: "text" as const, value } });

/**
 * A structured part, built as a real `google.protobuf.Value`.
 *
 * Not a plain object: `Part.data` is a bare `Value`, which protobuf-es represents as
 * a message with a `kind` oneof rather than flattening to JSON the way it does a
 * `Struct`. A fixture handing over a plain object is rejected outright — which is
 * the point, since the client reading one as if it were JSON is the bug this file
 * caught.
 */
const data = (value: Record<string, unknown>) => ({
  content: { case: "data" as const, value: fromJson(ValueSchema, value as never) },
});

/** A `working` status update carrying one message. */
function statusFrame(options: {
  taskId?: string;
  state?: TaskState;
  message?: {
    messageId: string;
    role: Role;
    parts: (ReturnType<typeof text> | ReturnType<typeof data>)[];
    metadata?: Record<string, unknown>;
  };
  seconds?: bigint;
}) {
  return {
    payload: {
      case: "statusUpdate" as const,
      value: {
        taskId: options.taskId ?? "task-1",
        contextId: CONVERSATION.id,
        status: {
          state: options.state ?? TaskState.WORKING,
          message: options.message,
          timestamp: { seconds: options.seconds ?? 1767225600n, nanos: 0 },
        },
      },
    },
  };
}

/** Serves one scripted turn and returns everything the client emitted. */
async function turn(frames: unknown[]): Promise<ChatEvent[]> {
  serve(({ service }) => {
    service(A2AService, {
      sendStreamingMessage: async function* () {
        for (const frame of frames) yield frame as never;
      },
    });
  });

  const events: ChatEvent[] = [];
  for await (const event of new A2AGrpcChatClient().send({
    conversation: CONVERSATION,
    text: "why is checkout crashlooping?",
  })) {
    events.push(event);
  }
  return events;
}

/** The messages a turn put on screen, with deltas folded into them as `useChat` does. */
function transcript(events: readonly ChatEvent[]): ChatMessage[] {
  const messages: ChatMessage[] = [];
  for (const event of events) {
    if (event.type === "message") {
      const at = messages.findIndex((message) => message.id === event.message.id);
      if (at === -1) messages.push(event.message);
      else messages[at] = event.message;
    }
    if (event.type === "delta") {
      const target = messages.find((message) => message.id === event.messageId);
      const first = target?.parts[0];
      if (target && first?.kind === "text") {
        target.parts = [{ kind: "text", text: first.text + event.text }];
      }
    }
  }
  return messages;
}

function textOf(message: ChatMessage): string {
  return message.parts
    .map((part) => (part.kind === "text" ? part.text : ""))
    .join("");
}

/**
 * What actually goes on the wire when a question is answered.
 *
 * Every assertion here is one of the four ways this fails **silently** — the turn
 * resumes, the agent replies, and the answer either never reached it or reached the
 * wrong question. There is no error to catch, so the request itself is the only
 * thing that can be measured.
 */
describe("A2AGrpcChatClient.send, answering a question", () => {
  /** Captures the request the client built, and ends the turn. */
  async function sentRequest(
    input: Partial<Parameters<A2AGrpcChatClient["send"]>[0]> = {},
  ): Promise<SendMessageRequest> {
    let captured: SendMessageRequest | undefined;
    serve(({ service }) => {
      service(A2AService, {
        // The turn ends without frames, which is what leaves the request itself as
        // the only thing to assert on.
        // eslint-disable-next-line require-yield
        sendStreamingMessage: async function* (request) {
          captured = request;
        },
      });
    });
    // Drained so the generator runs to completion; the frames are not the subject.
    for await (const event of new A2AGrpcChatClient().send({
      conversation: CONVERSATION,
      text: "Medium",
      ...input,
    })) {
      void event;
    }
    if (!captured) throw new Error("the client sent nothing");
    return captured;
  }

  it("activates the extension on the call, which is what makes a question answerable at all", async () => {
    /*
     * Measured on a cluster, and the least obvious thing here: the header matters on
     * the call that *asks*, not on the call that reads. A turn sent without it parks
     * with the question as bare prose and no correlation id — nothing to render and
     * nothing to answer against — and re-reading it with the header does not recover
     * what was never attached. So it goes on every call this client makes.
     */
    let headers: Record<string, string> | undefined;
    serve(({ service }) => {
      service(A2AService, {
        // See above.
        // eslint-disable-next-line require-yield
        sendStreamingMessage: async function* (_request, context) {
          headers = Object.fromEntries(context.requestHeader.entries());
        },
      });
    });
    for await (const event of new A2AGrpcChatClient().send({
      conversation: CONVERSATION,
      text: "anything",
    })) {
      void event;
    }
    expect(headers?.["a2a-extensions"]).toBe("https://kagent.dev/extensions/hitl/v1");
  });

  it("names the parked turn, so the answer resumes it instead of opening another", async () => {
    // Without the task id the gateway mints a fresh one, which it then refuses
    // because a question still stands — or, worse, starts a turn that leaves the
    // question unanswered while the send looks like it worked.
    const request = await sentRequest({ taskId: "parked-task" });
    expect(request.message?.taskId).toBe("parked-task");
  });

  it("declares the extension on the message, or the payload is never read", async () => {
    /*
     * `rawHitlMap` in `go/adk/pkg/a2a/hitl.go` checks `Extensions` before it looks
     * at `Metadata` at all. An answer that omits the declaration is forwarded to
     * the agent as ordinary text: the turn resumes, the agent replies, and the
     * structured answer was never delivered. Nothing reports it.
     */
    const request = await sentRequest({
      taskId: "parked-task",
      hitl: { "https://kagent.dev/extensions/hitl/v1": { type: "ask_user_response" } },
    });
    expect(request.message?.extensions).toEqual([
      "https://kagent.dev/extensions/hitl/v1",
    ]);
  });

  it("carries the answer under the extension's own URI, untouched", async () => {
    const payload = {
      "https://kagent.dev/extensions/hitl/v1": {
        type: "ask_user_response",
        id: "adk-1",
        answers: [{ answer: ["Medium"] }],
      },
    };
    const request = await sentRequest({ taskId: "parked-task", hitl: payload });
    expect(request.message?.metadata).toEqual(payload);
  });

  it("builds no ADK function_response parts — the runtime builds those", async () => {
    // A client-built one is rejected and the message forwarded as plain text, which
    // is the same silent failure again. The parts stay prose.
    const request = await sentRequest({
      taskId: "parked-task",
      hitl: { "https://kagent.dev/extensions/hitl/v1": { type: "ask_user_response" } },
    });
    expect(request.message?.parts).toHaveLength(1);
    expect(request.message?.parts[0]?.content.case).toBe("text");
  });

  it("declares nothing on an ordinary message", async () => {
    // The mirror, and the reason it is its own test: a message that declares the
    // extension with no payload under it, or names a task that is not parked, is
    // refused. An over-eager client would break every normal send.
    const request = await sentRequest();
    expect(request.message?.extensions).toEqual([]);
    expect(request.message?.taskId).toBe("");
  });
});

describe("A2AGrpcChatClient.send", () => {
  it("delivers the user's own message exactly once", async () => {
    const echoed = {
      messageId: "m-user",
      role: Role.USER,
      parts: [text("why is checkout crashlooping?")],
    };
    const events = await turn([
      statusFrame({ state: TaskState.SUBMITTED, message: echoed }),
      // The same message again, which the runtime does send: the transport is what
      // puts it on screen, so delivering it twice printed the reader's own question
      // twice.
      statusFrame({ state: TaskState.WORKING, message: echoed }),
    ]);

    const user = transcript(events).filter((message) => message.role === "user");
    expect(user).toHaveLength(1);
    expect(textOf(user[0])).toBe("why is checkout crashlooping?");
  });

  it("coalesces a reply streamed as partial chunks into one message", async () => {
    // Each chunk is a whole message with a messageId of its own — delivered as they
    // arrive, one reply became a column of bubbles, one per word. What relates them
    // is `adk_invocation_id`.
    const chunk = (id: string, value: string) => ({
      messageId: id,
      role: Role.AGENT,
      parts: [text(value)],
      metadata: { adk_partial: true, adk_invocation_id: "inv-1" },
    });

    const events = await turn([
      statusFrame({ message: chunk("c1", "The checkout ") }),
      statusFrame({ message: chunk("c2", "pod is out ") }),
      statusFrame({ message: chunk("c3", "of memory.") }),
    ]);

    const agent = transcript(events).filter((message) => message.role === "agent");
    expect(agent).toHaveLength(1);
    expect(textOf(agent[0])).toBe("The checkout pod is out of memory.");
  });

  it("replaces the streamed reply with the server's final one rather than adding it", async () => {
    // The complete message repeats every word already streamed, under a messageId of
    // its own. Emitted as a new message it printed the answer twice; emitted under
    // the streamed id it is a replacement, and the authority sits with the backend.
    const events = await turn([
      statusFrame({
        message: {
          messageId: "c1",
          role: Role.AGENT,
          parts: [text("The checkout ")],
          metadata: { adk_partial: true, adk_invocation_id: "inv-1" },
        },
      }),
      statusFrame({
        message: {
          messageId: "c2",
          role: Role.AGENT,
          parts: [text("pod is out of memory.")],
          metadata: { adk_partial: true, adk_invocation_id: "inv-1" },
        },
      }),
      statusFrame({
        state: TaskState.COMPLETED,
        message: {
          messageId: "final",
          role: Role.AGENT,
          parts: [text("The checkout pod is out of memory.")],
          metadata: { adk_invocation_id: "inv-1" },
        },
      }),
    ]);

    const agent = transcript(events).filter((message) => message.role === "agent");
    expect(agent).toHaveLength(1);
    expect(textOf(agent[0])).toBe("The checkout pod is out of memory.");
  });

  it("does not deliver the answer twice when an artifact repeats it", async () => {
    // The reply arrives as status text and again as the final artifact.
    const events = await turn([
      statusFrame({
        state: TaskState.COMPLETED,
        message: {
          messageId: "m-agent",
          role: Role.AGENT,
          parts: [text("3 pods are running.")],
        },
      }),
      {
        payload: {
          case: "artifactUpdate" as const,
          value: {
            taskId: "task-1",
            contextId: CONVERSATION.id,
            artifact: {
              artifactId: "a-1",
              parts: [text("3 pods are running.")],
            },
            lastChunk: true,
          },
        },
      },
    ]);

    const agent = transcript(events).filter((message) => message.role === "agent");
    expect(agent).toHaveLength(1);
  });

  it("still shows an artifact that says something new", async () => {
    // The dedupe is on the text, not on the shape, so an agent that only ever sends
    // artifacts still works.
    const events = await turn([
      {
        payload: {
          case: "artifactUpdate" as const,
          value: {
            taskId: "task-1",
            contextId: CONVERSATION.id,
            artifact: { artifactId: "a-1", parts: [text("Only in the artifact.")] },
            lastChunk: true,
          },
        },
      },
    ]);

    const agent = transcript(events).filter((message) => message.role === "agent");
    expect(agent).toHaveLength(1);
    expect(textOf(agent[0])).toBe("Only in the artifact.");
  });

  it("streams a reply arriving as appended artifact chunks", async () => {
    /*
     * The frames below are the shape captured from the controller on 2026-08-24,
     * for the prompt "Say exactly: alpha beta gamma" — one artifactId for the whole
     * reply, one frame per token, `append` on every frame after the first, and a
     * final frame repeating the whole answer.
     *
     * Read as whole messages under that shared id they replaced one another, so the
     * transcript showed a single token at a time and then the entire answer at the
     * moment the turn completed. That is the reported symptom: streaming that does
     * not preserve what it streamed.
     */
    const chunk = (value: string, flags: { append?: boolean; lastChunk?: boolean } = {}) => ({
      payload: {
        case: "artifactUpdate" as const,
        value: {
          taskId: "task-1",
          contextId: CONVERSATION.id,
          artifact: { artifactId: "a-1", parts: [text(value)] },
          ...flags,
        },
      },
    });

    const events = await turn([
      statusFrame({ state: TaskState.WORKING }),
      chunk("alpha"),
      chunk(" beta", { append: true }),
      chunk(" gamma", { append: true }),
      chunk("alpha beta gamma", { lastChunk: true }),
      statusFrame({ state: TaskState.COMPLETED }),
    ]);

    const agent = transcript(events).filter((message) => message.role === "agent");
    expect(agent, "one reply, not one message per token").toHaveLength(1);
    expect(textOf(agent[0])).toBe("alpha beta gamma");

    // And it was *streamed*, not delivered whole at the end. Without this the test
    // above passes on a client that shows nothing until the final frame, which is
    // exactly the behaviour being fixed.
    expect(
      events.filter((event) => event.type === "delta").map((event) => event.text),
      "each appended chunk should reach the transcript as it arrives",
    ).toEqual([" beta", " gamma"]);
  });

  it("does not double the answer when the last chunk repeats what was streamed", async () => {
    // The closing frame carries the whole reply rather than the last increment, and
    // is not flagged `append` — so it is a replacement. Appending it instead would
    // print the answer one and a half times.
    const chunk = (value: string, flags: { append?: boolean; lastChunk?: boolean } = {}) => ({
      payload: {
        case: "artifactUpdate" as const,
        value: {
          taskId: "task-1",
          contextId: CONVERSATION.id,
          artifact: { artifactId: "a-1", parts: [text(value)] },
          ...flags,
        },
      },
    });

    const events = await turn([
      chunk("one "),
      chunk("two", { append: true }),
      chunk("one two", { lastChunk: true }),
    ]);

    const agent = transcript(events).filter((message) => message.role === "agent");
    expect(agent).toHaveLength(1);
    expect(textOf(agent[0]), "not \"one twoone two\"").toBe("one two");
    // The closing frame replaces rather than appends, so it carries no delta.
    expect(
      events.filter((event) => event.type === "delta").map((event) => event.text),
    ).toEqual(["two"]);
  });

  it("labels a tool call and its result distinguishably", async () => {
    const events = await turn([
      statusFrame({
        message: {
          messageId: "call-1",
          role: Role.AGENT,
          parts: [data({ name: "k8s_get_pods", args: { namespace: "shop" } })],
        },
      }),
      statusFrame({
        message: {
          messageId: "result-1",
          role: Role.AGENT,
          parts: [data({ name: "k8s_get_pods", response: { count: 3 } })],
        },
      }),
    ]);

    const parts = transcript(events).flatMap((message) => message.parts);
    const kinds = parts.map((part) => (part.kind === "data" ? part.dataKind : part.kind));
    expect(kinds).toEqual(["tool_call", "tool_result"]);
  });

  it("reports the turn reaching completion", async () => {
    const events = await turn([
      statusFrame({
        state: TaskState.COMPLETED,
        message: { messageId: "m1", role: Role.AGENT, parts: [text("done")] },
      }),
    ]);

    const states = events
      .filter((event) => event.type === "status")
      .map((event) => (event.type === "status" ? event.state : ""));
    expect(states.at(-1)).toBe("completed");
  });

  it("reports a failed turn as failed rather than leaving it working", async () => {
    const events = await turn([
      statusFrame({
        state: TaskState.FAILED,
        message: { messageId: "m1", role: Role.AGENT, parts: [text("no such tool")] },
      }),
    ]);

    const states = events
      .filter((event) => event.type === "status")
      .map((event) => (event.type === "status" ? event.state : ""));
    expect(states).toContain("failed");
  });

  it("addresses the conversation by contextId and routes on the instance headers", async () => {
    let sent: SendMessageRequest | undefined;
    let namespaceHeader: string | null = null;
    let idHeader: string | null = null;

    serve(({ service }) => {
      service(A2AService, {
        // eslint-disable-next-line require-yield
        sendStreamingMessage: async function* (request, context) {
          sent = request;
          namespaceHeader = context.requestHeader.get(
            "x-kagent-agent-instance-namespace",
          );
          idHeader = context.requestHeader.get("x-kagent-agent-instance-id");
        },
      });
    });

    // Drained so the call completes; what is asserted is what the server received.
    for await (const event of new A2AGrpcChatClient().send({
      conversation: CONVERSATION,
      text: "hello",
    })) {
      expect(event).toBeDefined();
    }

    // Both halves of the address, because the gateway routes on the metadata rather
    // than on a path — a gRPC method has no path to put them in.
    expect(namespaceHeader).toBe(CONVERSATION.namespace);
    expect(idHeader).toBe(CONVERSATION.id);
    // The instance's own id is the conversation's context, and the gateway refuses a
    // value that is neither empty nor its own.
    expect(sent?.message?.contextId).toBe(CONVERSATION.id);
    expect(sent?.message?.role).toBe(Role.USER);
  });
});

describe("A2AGrpcChatClient.history", () => {
  function serveTasks(tasks: unknown[]): void {
    serve(({ service }) => {
      service(A2AService, {
        listTasks: () => ({ tasks, nextPageToken: "" }) as never,
      });
    });
  }

  it("orders messages and artifacts by their timeline positions", async () => {
    const position = (value: string) => ({ "kagent.dev/timeline-position": value });
    serveTasks([
      {
        id: "task-1",
        contextId: CONVERSATION.id,
        status: { state: TaskState.COMPLETED, timestamp: { seconds: 1767225600n } },
        history: [
          { messageId: "u0", role: Role.USER, parts: [text("start")], metadata: position("1") },
          { messageId: "u1", role: Role.USER, parts: [text("answer")], metadata: position("4") },
        ],
        artifacts: [
          { artifactId: "a0", parts: [data({ name: "ask_user" })], metadata: position("2") },
          { artifactId: "a1", parts: [text("Which topic?")], metadata: position("3") },
          { artifactId: "a2", parts: [text("Thanks")], metadata: position("5") },
        ],
      },
    ]);

    const { messages } = await new A2AGrpcChatClient().history(CONVERSATION);
    expect(messages.map((message) => message.id)).toEqual(["u0", "a0", "a1", "u1", "a2"]);
  });

  it("keeps a tool call and its result apart when replaying", async () => {
    // Consecutive agent messages carrying data parts and no text at all: comparing
    // text made them look identical and dropped the result, so a replayed
    // conversation showed every finished tool call still waiting to return.
    serveTasks([
      {
        id: "task-1",
        contextId: CONVERSATION.id,
        status: { state: TaskState.COMPLETED, timestamp: { seconds: 1767225600n } },
        history: [
          {
            messageId: "call-1",
            role: Role.AGENT,
            parts: [data({ name: "k8s_get_pods", args: {} })],
          },
          {
            messageId: "result-1",
            role: Role.AGENT,
            parts: [data({ name: "k8s_get_pods", response: { count: 3 } })],
          },
        ],
        artifacts: [],
      },
    ]);

    const { messages } = await new A2AGrpcChatClient().history(CONVERSATION);
    expect(messages).toHaveLength(2);
  });

  it("dates a replayed message by the turn, not by when it was reopened", async () => {
    serveTasks([
      {
        id: "task-1",
        contextId: CONVERSATION.id,
        status: { state: TaskState.COMPLETED, timestamp: { seconds: 1767225600n } },
        history: [{ messageId: "m1", role: Role.USER, parts: [text("hello")] }],
        artifacts: [],
      },
    ]);

    const { messages: [message] } = await new A2AGrpcChatClient().history(CONVERSATION);
    expect(message.createdAt).toBe("2026-01-01T00:00:00.000Z");
  });

  it("collapses the opening message the gateway repeats", async () => {
    // The first user message appears in `history` and again as the status message.
    serveTasks([
      {
        id: "task-1",
        contextId: CONVERSATION.id,
        status: {
          state: TaskState.COMPLETED,
          timestamp: { seconds: 1767225600n },
          message: { messageId: "m1", role: Role.USER, parts: [text("hello")] },
        },
        history: [{ messageId: "m1", role: Role.USER, parts: [text("hello")] }],
        artifacts: [],
      },
    ]);

    const { messages } = await new A2AGrpcChatClient().history(CONVERSATION);
    expect(messages).toHaveLength(1);
  });

  it("names a message the same thing on a re-read, so a merge can recognise it", async () => {
    /*
     * Reported as a defect: in a conversation several turns long, tabbing away and
     * back put an earlier agent reply underneath the newest one, and a refresh
     * cleared it.
     *
     * The gateway does not name an agent reply, and the id for an unnamed one came
     * from a process-wide counter — so the same reply was `history-1` on one read and
     * `history-2` on the next. `useLiveTranscript` re-reads on `visibilitychange`,
     * the transcript merge treats the id as identity, and a copy that no longer
     * matched was kept as a local addition and appended after everything the server
     * sent. A reload dropped the copy and reset the counter together, which is why it
     * looked like a rendering fault rather than a merge.
     *
     * Asserted across two reads of the *same* task, because one read cannot show it.
     */
    const task = {
      id: "task-1",
      contextId: CONVERSATION.id,
      status: { state: TaskState.COMPLETED, timestamp: { seconds: 1767225600n } },
      history: [
        { messageId: "m1", role: Role.USER, parts: [text("how many pods?")] },
        // Unnamed, as an agent reply arrives.
        { role: Role.AGENT, parts: [text("3 pods")] },
      ],
      artifacts: [{ parts: [text("and one pending")] }],
    };

    serveTasks([task]);
    const first = await new A2AGrpcChatClient().history(CONVERSATION);
    serveTasks([task]);
    const second = await new A2AGrpcChatClient().history(CONVERSATION);

    expect(second.messages.map((message) => message.id)).toEqual(
      first.messages.map((message) => message.id),
    );
    // And the derived ids are still distinct from each other, or the merge would
    // collapse two different messages into one.
    expect(new Set(first.messages.map((message) => message.id)).size).toBe(
      first.messages.length,
    );
  });

  it("drops an artifact repeating text already in the history", async () => {
    serveTasks([
      {
        id: "task-1",
        contextId: CONVERSATION.id,
        status: { state: TaskState.COMPLETED, timestamp: { seconds: 1767225600n } },
        history: [{ messageId: "m1", role: Role.AGENT, parts: [text("3 pods")] }],
        artifacts: [{ artifactId: "a-1", parts: [text("3 pods")] }],
      },
    ]);

    const { messages } = await new A2AGrpcChatClient().history(CONVERSATION);
    expect(messages).toHaveLength(1);
  });

  it("follows every page of a long conversation", async () => {
    // A conversation shown with its first page only, saying nothing, is the quiet
    // half-truth this codebase keeps having to undo.
    const page = (id: string, nextPageToken: string) => ({
      tasks: [
        {
          id,
          contextId: CONVERSATION.id,
          status: { state: TaskState.COMPLETED, timestamp: { seconds: 1767225600n } },
          history: [{ messageId: `m-${id}`, role: Role.USER, parts: [text(id)] }],
          artifacts: [],
        },
      ],
      nextPageToken,
    });

    serve(({ service }) => {
      service(A2AService, {
        listTasks: ((request: { pageToken: string }) =>
          request.pageToken === ""
            ? page("task-1", "token-2")
            : page("task-2", "")) as never,
      });
    });

    const { messages } = await new A2AGrpcChatClient().history(CONVERSATION);
    expect(messages.map((message) => textOf(message))).toEqual(["task-1", "task-2"]);
  });
});
