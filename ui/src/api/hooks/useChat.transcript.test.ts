import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { resetChatClient, setChatClientFactory } from "../chat";
import type { PendingRequest } from "../chat/hitl";
import type {
  ChatClient,
  ChatEvent,
  ChatMessage,
  SendMessageInput,
} from "../chat/types";
import { useChat } from "./useChat";

/**
 * The reader's own message, and what happens to it afterwards.
 *
 * The defect these cover was reported from a cluster and invisible in fixtures:
 * the transcript showed only the agent's replies, and the reader's questions
 * appeared only after a reload. The cause was that this hook waited to be told its
 * own message existed, and the A2A gateway never tells it — measured on
 * 2026-08-24, a completed turn emits a `WORKING` frame and a `COMPLETED` frame and
 * no message frame at all, while `ListTasks` afterwards holds the user's message
 * and nothing else.
 *
 * So every client below is **silent about the user's message**, the way the
 * gateway is. A client that echoed it would pass these tests without the hook
 * doing anything, which is exactly how the browser suite came to be green over
 * this.
 */

const CONVERSATION = { namespace: "kagent", id: "instance-1" };
const QUESTION = "why is checkout crashlooping?";

/** The text of a message, for asserting on what is on screen rather than on shape. */
function textOf(message: ChatMessage): string {
  return message.parts
    .map((part) => (part.kind === "text" ? part.text : ""))
    .join("");
}

/** A transport that records what it was asked and answers however the test says. */
function client(options: {
  history?: ChatMessage[] | (() => Promise<ChatMessage[]>);
  awaitingReply?: PendingRequest;
  reply?: (input: SendMessageInput) => ChatEvent[];
  seen?: SendMessageInput[];
}): ChatClient {
  return {
    protocolVersion: "test",
    history: async () => ({
      messages:
        typeof options.history === "function"
          ? await options.history()
          : (options.history ?? []),
      awaitingReply: options.awaitingReply,
    }),
    cancel: async () => {},
    send: (input) =>
      (async function* (): AsyncIterable<ChatEvent> {
        options.seen?.push(input);
        for (const event of options.reply?.(input) ?? []) yield event;
      })(),
  };
}

afterEach(() => {
  resetChatClient();
});

describe("useChat and the reader's own message", () => {
  it("puts it in the transcript on send, without waiting for the server", async () => {
    // The exact shape of the cluster's turn: two status frames and no content.
    setChatClientFactory(() =>
      client({
        reply: () => [
          { type: "status", state: "working", taskId: "task-1" },
          { type: "status", state: "completed", taskId: "task-1" },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));

    await act(async () => {
      await result.current.send(QUESTION);
    });

    const mine = result.current.messages.filter((message) => message.role === "user");
    expect(mine).toHaveLength(1);
    expect(textOf(mine[0])).toBe(QUESTION);
  });

  it("sends it under the id it filed it under, which is what lets an echo replace it", async () => {
    const seen: SendMessageInput[] = [];
    setChatClientFactory(() => client({ seen, reply: () => [] }));

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    expect(seen).toHaveLength(1);
    // Not merely "some id": the id on screen and the id on the wire have to be the
    // same one, or an echo arrives as a second copy of the same sentence.
    expect(seen[0].messageId).toBe(result.current.messages[0].id);
  });

  it("upserts a gateway's echo rather than showing the question twice", async () => {
    // Not every gateway is silent — the shape of the wire allows an echo, and one
    // arriving must land on what is already there. This is the failure optimism
    // without a stable key produces, and it is silent: two identical bubbles read
    // as the reader having sent it twice.
    setChatClientFactory(() =>
      client({
        reply: (input) => [
          {
            type: "message",
            message: {
              id: input.messageId ?? "server-id",
              role: "user",
              parts: [{ kind: "text", text: input.text }],
              createdAt: new Date().toISOString(),
            },
          },
          { type: "status", state: "completed", taskId: "task-1" },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    expect(result.current.messages.filter((m) => m.role === "user")).toHaveLength(1);
  });

  it("keeps both questions when a second is asked, in the order they were asked", async () => {
    setChatClientFactory(() =>
      client({
        reply: (input) => [
          { type: "status", state: "working", taskId: "task" },
          {
            type: "message",
            message: {
              id: `reply-to-${input.messageId}`,
              role: "agent",
              parts: [{ kind: "text", text: "because its liveness probe fails" }],
              createdAt: new Date().toISOString(),
            },
          },
          { type: "status", state: "completed", taskId: "task" },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));

    await act(async () => {
      await result.current.send(QUESTION);
    });
    await act(async () => {
      await result.current.send("and how do I fix it?");
    });

    expect(result.current.messages.map((m) => `${m.role}: ${textOf(m)}`)).toEqual([
      `user: ${QUESTION}`,
      "agent: because its liveness probe fails",
      "user: and how do I fix it?",
      "agent: because its liveness probe fails",
    ]);
  });

  it("does not lose a message sent while the history read was still in flight", async () => {
    /*
     * A slow history behind a fast composer. The read resolves *after* the send,
     * and assigning its result would replace the transcript with the server's
     * older copy — taking the reader's message off the screen again, which is the
     * reported bug arriving by a different door.
     */
    let release: (() => void) | undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });

    setChatClientFactory(() =>
      client({
        history: async () => {
          await held;
          return [
            {
              id: "old-1",
              role: "user",
              parts: [{ kind: "text", text: "something asked yesterday" }],
              createdAt: "2026-08-23T10:00:00Z",
            },
          ];
        },
        reply: () => [{ type: "status", state: "completed", taskId: "task-1" }],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));

    await act(async () => {
      await result.current.send(QUESTION);
    });
    expect(result.current.messages.map(textOf)).toEqual([QUESTION]);

    await act(async () => {
      release?.();
      await held;
    });

    await waitFor(() => {
      // History first, because it is older; the local message kept, because
      // nothing else has it.
      expect(result.current.messages.map(textOf)).toEqual([
        "something asked yesterday",
        QUESTION,
      ]);
    });
  });

  it("does not show a message twice when the server's history already carries it", async () => {
    // The other half of the same merge: the reader sends, the history read lands
    // afterwards with that very message in it. Same id, so it is recognised as the
    // one on screen rather than appended beside it.
    let release: (() => void) | undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    const seen: SendMessageInput[] = [];

    setChatClientFactory(() =>
      client({
        seen,
        history: async () => {
          await held;
          return [
            {
              id: seen[0]?.messageId ?? "unsent",
              role: "user",
              parts: [{ kind: "text", text: QUESTION }],
              createdAt: "2026-08-24T10:00:00Z",
            },
          ];
        },
        reply: () => [{ type: "status", state: "completed", taskId: "task-1" }],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    await act(async () => {
      release?.();
      await held;
    });

    await waitFor(() => {
      expect(result.current.messages.map(textOf)).toEqual([QUESTION]);
    });
  });

  it("takes a refused message back off the screen", async () => {
    /*
     * The commonest refusal is a question the agent is still waiting on: the
     * controller answers `FailedPrecondition` and files nothing. Leaving the
     * optimistic copy on screen would claim a turn that does not exist — and it
     * vanishes on the next reload, which is the same class of lie as the message
     * that only appeared on one.
     */
    setChatClientFactory(() =>
      client({
        reply: () => [
          {
            type: "error",
            error: new Error(
              "the agent is waiting for a reply to its last message; answer it, or cancel that task to start a new one",
            ),
          },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    expect(result.current.messages).toHaveLength(0);
    expect(result.current.turnPhase).toBe("failed");
    expect(result.current.turnError?.message).toMatch(/waiting for a reply/);
  });

  it("keeps a message whose turn failed after it had started", async () => {
    // The other side of the line above. A turn the server took and then could not
    // finish did happen, and the message it was about belongs in the transcript —
    // which is also where Retry needs it to be.
    setChatClientFactory(() =>
      client({
        reply: () => [
          { type: "status", state: "working", taskId: "task-1" },
          { type: "error", error: new Error("the runtime went away") },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    expect(result.current.messages.map(textOf)).toEqual([QUESTION]);
  });
});

describe("useChat and a question the conversation is holding", () => {
  it("reports one read out of history, because a reload lands back in it", async () => {
    // The parked turn is a property of the *task*, not of anything on screen: the
    // question renders as ordinary agent prose, so without this a reopened
    // conversation holding one looks finished and refuses the next message.
    setChatClientFactory(() =>
      client({
        history: [
          {
            id: "asked",
            role: "agent",
            parts: [{ kind: "text", text: "Which topping would you like?" }],
            createdAt: "2026-08-24T10:00:00Z",
          },
        ],
        awaitingReply: { kind: "unknown", taskId: "parked-task" },
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));

    expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" });
    // And the turn itself is idle — nothing is running, which is exactly why the
    // page cannot learn this from the turn.
    expect(result.current.turnPhase).toBe("idle");
  });

  it("reports one that parks while the page is watching", async () => {
    setChatClientFactory(() =>
      client({
        reply: () => [
          { type: "status", state: "working", taskId: "task-9" },
          { type: "status", state: "input_required", taskId: "task-9" },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    // `unknown` until the history read fills it in: the status event reports the
    // state, and what is being asked rides on the message that came with it.
    expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "task-9" });
    expect(result.current.turnPhase).toBe("awaiting_input");
    // The turn is over: the agent has stopped and will not continue on its own, so
    // the composer must come back rather than sit behind a spinner forever.
    expect(result.current.phase).toBe("idle");
  });

  it("answers it by naming the parked turn, which is what makes it an answer", async () => {
    /*
     * The whole of the difference between answering and asking something new.
     *
     * `prepareSend` accepts a task id only when it names the instance's own active
     * task and that task is waiting on the reader — and without one it mints a fresh
     * task, which opens a second turn and leaves the question unanswered while the
     * send looks like it worked. So the id travelling is the behaviour, not a detail.
     */
    const seen: SendMessageInput[] = [];
    setChatClientFactory(() =>
      client({
        seen,
        awaitingReply: { kind: "unknown", taskId: "parked-task" },
        reply: () => [{ type: "status", state: "completed", taskId: "parked-task" }],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() =>
      expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" }),
    );

    await act(async () => {
      await result.current.send("Pineapple, obviously");
    });

    expect(seen[0].taskId).toBe("parked-task");
    // And the question is no longer outstanding, so the page stops asking for one.
    expect(result.current.pendingQuestion).toBeUndefined();
  });

  it("renders an ask_user answer as a semantic record instead of response prose", async () => {
    setChatClientFactory(() =>
      client({
        awaitingReply: {
          kind: "ask_user",
          taskId: "parked-task",
          requestId: "ask-1",
          questions: [
            { question: "What size?", choices: ["Small", "Large"], multiple: false },
          ],
        },
        reply: () => [{ type: "status", state: "completed", taskId: "parked-task" }],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.pendingQuestion?.kind).toBe("ask_user"));
    await act(async () => {
      await result.current.answerQuestion([["Large"]]);
    });

    expect(result.current.messages.at(-1)?.parts).toEqual([
      {
        kind: "ask_user",
        interaction: {
          questions: [
            { question: "What size?", choices: ["Small", "Large"], multiple: false },
          ],
          answers: [["Large"]],
          askedBy: undefined,
        },
      },
    ]);
  });

  it("does not name a turn on an ordinary message", async () => {
    // The mirror of the above, and the reason it is a separate test: a task id sent
    // for a turn that is not parked is refused rather than quietly misfiled, so an
    // over-eager client would break every normal message.
    const seen: SendMessageInput[] = [];
    setChatClientFactory(() =>
      client({ seen, reply: () => [{ type: "status", state: "completed", taskId: "t" }] }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    await act(async () => {
      await result.current.send(QUESTION);
    });

    expect(seen[0].taskId).toBeUndefined();
  });

  it("resumes a parked tool call with its structured approval decision", async () => {
    const seen: SendMessageInput[] = [];
    setChatClientFactory(() =>
      client({
        seen,
        awaitingReply: {
          kind: "tool_approval",
          taskId: "parked-task",
          tools: [{ id: "approval-7", name: "k8s_get_resources", args: {} }],
        },
        reply: () => [
          { type: "status", state: "completed", taskId: "parked-task" },
        ],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(result.current.pendingQuestion?.kind).toBe("tool_approval"));

    await act(async () => {
      await result.current.answerToolApproval([
        {
          id: "approval-7",
          approved: false,
          rejectionReason: "Production is still serving traffic",
        },
      ]);
    });

    expect(seen[0]).toMatchObject({
      taskId: "parked-task",
      text: "Rejected: k8s_get_resources\nReason: Production is still serving traffic",
      hitl: {
        "https://kagent.dev/extensions/hitl/v1": {
          type: "tool_approval_response",
          approvals: [
            {
              id: "approval-7",
              approved: false,
              rejection_reason: "Production is still serving traffic",
            },
          ],
        },
      },
    });
    expect(result.current.messages.at(-1)?.parts).toEqual([
      {
        kind: "tool_approval",
        approval: {
          tools: [{ id: "approval-7", name: "k8s_get_resources", args: {} }],
          decisions: [
            {
              id: "approval-7",
              approved: false,
              rejectionReason: "Production is still serving traffic",
            },
          ],
          askedBy: undefined,
        },
      },
    ]);
  });

  it("puts the question back when the answer was refused", async () => {
    // The notice is cleared optimistically, because a turn that resumes is no longer
    // parked. If the send is refused the question is still standing, and a page that
    // had quietly dropped the notice would leave the reader with no way to answer it
    // and no explanation.
    setChatClientFactory(() =>
      client({
        awaitingReply: { kind: "unknown", taskId: "parked-task" },
        reply: () => [{ type: "error", error: new Error("that turn already moved on") }],
      }),
    );

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() =>
      expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" }),
    );

    await act(async () => {
      await result.current.send("Pineapple, obviously");
    });

    expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" });
  });

  it("gives the question up by cancelling its task, and re-reads rather than assuming", async () => {
    /*
     * Cancelling is the only way out — answering is not routable over this
     * protocol — so the state afterwards is read back rather than presumed. A page
     * that assumed success would offer a composer the controller still refuses.
     */
    const cancelled: string[] = [];
    let parked: PendingRequest | undefined = { kind: "unknown", taskId: "parked-task" };

    setChatClientFactory(() => ({
      protocolVersion: "test",
      history: async () => ({ messages: [], awaitingReply: parked }),
      cancel: async (_conversation, taskId) => {
        cancelled.push(taskId);
        parked = undefined;
      },
      send: () =>
        (async function* (): AsyncIterable<ChatEvent> {
          // Not reached in this test.
        })(),
    }));

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() =>
      expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" }),
    );

    await act(async () => {
      await result.current.dismissQuestion();
    });

    expect(cancelled, "the parked task is what has to be cancelled").toEqual([
      "parked-task",
    ]);
    expect(result.current.pendingQuestion).toBeUndefined();
  });

  it("reports a cancellation that failed instead of pretending the way out worked", async () => {
    setChatClientFactory(() => ({
      protocolVersion: "test",
      history: async () => ({ messages: [], awaitingReply: { kind: "unknown", taskId: "parked-task" } }),
      cancel: async () => {
        throw new Error("the runtime refused the cancellation");
      },
      send: () =>
        (async function* (): AsyncIterable<ChatEvent> {
          // Not reached in this test.
        })(),
    }));

    const { result } = renderHook(() => useChat(CONVERSATION));
    await waitFor(() =>
      expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" }),
    );

    await act(async () => {
      await result.current.dismissQuestion();
    });

    // Still parked, and the failure is on screen. Unlike `cancel`, this one is not
    // best-effort: it is the only exit, so swallowing a failure would leave the
    // reader clicking a button that silently does nothing.
    expect(result.current.pendingQuestion).toEqual({ kind: "unknown", taskId: "parked-task" });
    expect(result.current.turnError?.message).toMatch(/refused the cancellation/);
  });
});

describe("useChat, continued", () => {
  it("leaves the transcript of a conversation the reader has left alone", async () => {
    // A turn outlives the page that started it. Its events must not write into
    // whichever conversation is on screen by the time they arrive.
    setChatClientFactory(() => client({ reply: () => [] }));

    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat({ namespace: "kagent", id }),
      { initialProps: { id: "instance-1" } },
    );
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));

    await act(async () => {
      await result.current.send(QUESTION);
    });
    expect(result.current.messages).toHaveLength(1);

    rerender({ id: "instance-2" });
    await waitFor(() => expect(result.current.isLoadingHistory).toBe(false));
    expect(result.current.messages).toHaveLength(0);
    expect(result.current.turnPhase).toBe("idle");
  });
});
