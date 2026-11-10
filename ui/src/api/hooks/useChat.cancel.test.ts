import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetChatClient, setChatClientFactory } from "../chat";
import type { ChatClient, ChatEvent } from "../chat/types";
import { useChat } from "./useChat";

const CONVERSATION = { id: "instance-1", agent: "team-a/assistant" };

/** A stream the test feeds by hand, so ordering never depends on timers. */
function controlledStream() {
  const queue: ChatEvent[] = [];
  let wake: (() => void) | undefined;
  let finished = false;
  const push = (event: ChatEvent) => {
    queue.push(event);
    wake?.();
  };
  const end = () => {
    finished = true;
    wake?.();
  };
  const iterable = async function* (): AsyncIterable<ChatEvent> {
    for (;;) {
      if (queue.length) {
        yield queue.shift()!;
      } else if (finished) {
        return;
      } else {
        await new Promise<void>((resolve) => (wake = resolve));
      }
    }
  };
  return { push, end, iterable };
}

function setup() {
  const stream = controlledStream();
  const cancel = vi.fn<ChatClient["cancel"]>(async () => {});
  let signal: AbortSignal | undefined;
  setChatClientFactory(() => ({
    protocolVersion: "test",
    history: async () => ({ messages: [] }),
    cancel,
    send: (input) => {
      signal = input.signal;
      return stream.iterable();
    },
  }));
  const hook = renderHook(() => useChat(CONVERSATION));
  return { stream, cancel, hook, isAborted: () => signal?.aborted };
}

describe("useChat cancel", () => {
  afterEach(() => resetChatClient());

  it("cancels the task once the first frame names it, when stop came first", async () => {
    const { stream, cancel, hook, isAborted } = setup();

    await act(async () => {
      void hook.result.current.send("restart the deployment");
    });
    await act(async () => {
      await hook.result.current.cancel();
    });

    // The page answers at once; the server has not been told yet.
    expect(hook.result.current.turnState).toBe("canceled");
    expect(cancel).not.toHaveBeenCalled();

    await act(async () => {
      stream.push({ type: "status", state: "working", taskId: "task-1" });
    });
    await waitFor(() => expect(cancel).toHaveBeenCalledTimes(1));
    expect(cancel).toHaveBeenCalledWith(CONVERSATION, "task-1");

    await act(async () => {
      stream.push({ type: "status", state: "completed", taskId: "task-1" });
      stream.end();
    });
    expect(hook.result.current.turnState).toBe("canceled");
    expect(cancel).toHaveBeenCalledTimes(1);
    expect(isAborted()).toBe(true);
  });

  it("cancels once when the task is already named", async () => {
    const { stream, cancel, hook } = setup();

    await act(async () => {
      void hook.result.current.send("restart the deployment");
    });
    await act(async () => {
      stream.push({ type: "status", state: "working", taskId: "task-1" });
    });
    await act(async () => {
      await hook.result.current.cancel();
    });

    expect(cancel).toHaveBeenCalledTimes(1);
    expect(cancel).toHaveBeenCalledWith(CONVERSATION, "task-1");
    expect(hook.result.current.turnState).toBe("canceled");
  });

  it("sends nothing when the stream ends without ever naming a task", async () => {
    const { stream, cancel, hook } = setup();

    await act(async () => {
      void hook.result.current.send("restart the deployment");
    });
    await act(async () => {
      await hook.result.current.cancel();
    });
    await act(async () => {
      stream.end();
    });

    expect(cancel).not.toHaveBeenCalled();
    expect(hook.result.current.turnState).toBe("canceled");
  });

  it("takes back a refused answer and restores the question when stop came first", async () => {
    const parked = { kind: "unknown" as const, taskId: "parked-task" };
    const cancel = vi.fn<ChatClient["cancel"]>(async () => {});
    let refuse: (() => void) | undefined;
    setChatClientFactory(() => ({
      protocolVersion: "test",
      history: async () => ({ messages: [], awaitingReply: parked }),
      cancel,
      send: () =>
        (async function* (): AsyncIterable<ChatEvent> {
          await new Promise<void>((resolve) => (refuse = resolve));
          yield* [] as ChatEvent[];
          throw new Error("a question is still waiting");
        })(),
    }));
    const hook = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(hook.result.current.pendingQuestion).toEqual(parked));

    await act(async () => {
      void hook.result.current.send("my answer");
    });
    await act(async () => {
      await hook.result.current.cancel();
    });
    await act(async () => {
      refuse?.();
    });

    expect(hook.result.current.messages.filter((m) => m.role === "user")).toEqual([]);
    expect(hook.result.current.pendingQuestion).toEqual(parked);
    expect(cancel).not.toHaveBeenCalled();
    expect(hook.result.current.turnPhase).not.toBe("working");
    expect(hook.result.current.turnState).not.toBe("working");
  });

  it("takes back a refused answer when the stream yields an error event after stop", async () => {
    const parked = { kind: "unknown" as const, taskId: "parked-task" };
    let refuse: (() => void) | undefined;
    setChatClientFactory(() => ({
      protocolVersion: "test",
      history: async () => ({ messages: [], awaitingReply: parked }),
      cancel: async () => {},
      send: () =>
        (async function* (): AsyncIterable<ChatEvent> {
          await new Promise<void>((resolve) => (refuse = resolve));
          yield { type: "error", error: new Error("a question is still waiting") };
        })(),
    }));
    const hook = renderHook(() => useChat(CONVERSATION));
    await waitFor(() => expect(hook.result.current.pendingQuestion).toEqual(parked));

    await act(async () => {
      void hook.result.current.send("my answer");
    });
    await act(async () => {
      await hook.result.current.cancel();
    });
    await act(async () => {
      refuse?.();
    });

    expect(hook.result.current.messages.filter((m) => m.role === "user")).toEqual([]);
    expect(hook.result.current.pendingQuestion).toEqual(parked);
    expect(hook.result.current.turnPhase).not.toBe("working");
    expect(hook.result.current.turnState).not.toBe("working");
  });

  it("leaves another conversation's question alone when a refused answer is restored", async () => {
    const other = { id: "instance-2", agent: "team-a/assistant" };
    const q1 = { kind: "unknown" as const, taskId: "task-1" };
    const q2 = { kind: "unknown" as const, taskId: "task-2" };
    let refuse: (() => void) | undefined;
    setChatClientFactory(() => ({
      protocolVersion: "test",
      history: async (conversation) => ({
        messages: [],
        awaitingReply: conversation.id === CONVERSATION.id ? q1 : q2,
      }),
      cancel: async () => {},
      send: () =>
        (async function* (): AsyncIterable<ChatEvent> {
          await new Promise<void>((resolve) => (refuse = resolve));
          yield* [] as ChatEvent[];
          throw new Error("a question is still waiting");
        })(),
    }));
    const hook = renderHook((conversation) => useChat(conversation), {
      initialProps: CONVERSATION,
    });
    await waitFor(() => expect(hook.result.current.pendingQuestion).toEqual(q1));

    await act(async () => {
      void hook.result.current.send("my answer");
    });
    await act(async () => {
      await hook.result.current.cancel();
    });
    hook.rerender(other);
    await waitFor(() => expect(hook.result.current.pendingQuestion).toEqual(q2));
    await act(async () => {
      refuse?.();
    });

    expect(hook.result.current.pendingQuestion).toEqual(q2);
  });

  it("keeps a new run going when the stream stopped early later throws", async () => {
    const second = controlledStream();
    let failFirst: (() => void) | undefined;
    let sends = 0;
    const cancel = vi.fn<ChatClient["cancel"]>(async () => {});
    setChatClientFactory(() => ({
      protocolVersion: "test",
      history: async () => ({ messages: [] }),
      cancel,
      send: () =>
        ++sends === 1
          ? (async function* (): AsyncIterable<ChatEvent> {
              await new Promise<void>((resolve) => (failFirst = resolve));
              yield* [] as ChatEvent[];
              throw new Error("the first stream broke");
            })()
          : second.iterable(),
    }));
    const hook = renderHook(() => useChat(CONVERSATION));

    await act(async () => {
      void hook.result.current.send("first");
    });
    await act(async () => {
      await hook.result.current.cancel();
    });
    await act(async () => {
      void hook.result.current.send("second");
    });
    await act(async () => {
      failFirst?.();
    });

    expect(hook.result.current.turnState).not.toBe("failed");

    await act(async () => {
      second.push({ type: "status", state: "working", taskId: "task-2" });
    });
    expect(hook.result.current.turnState).toBe("working");
    expect(hook.result.current.messages.filter((m) => m.role === "user")).toHaveLength(
      1,
    );
    expect(cancel).not.toHaveBeenCalled();
  });
});
