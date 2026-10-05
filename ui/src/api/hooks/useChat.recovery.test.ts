import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { resetChatClient, setChatClientFactory } from "../chat";
import { ApiError } from "../ApiError";
import type { ChatClient, ChatEvent, ChatHistory } from "../chat/types";
import { useChat } from "./useChat";

const SESSION = { id: "session-1", agent: "team/agent" };
const working: ChatHistory = { messages: [], turn: { taskId: "task-1", state: "working" } };
const completed: ChatHistory = {
  messages: [{ id: "final", role: "agent", parts: [{ kind: "text", text: "Tests passed" }], createdAt: "2026-10-01T10:00:00Z", taskId: "task-1" }],
  turn: { taskId: "task-1", state: "completed" },
};
function install(read: () => Promise<ChatHistory>, stream?: () => AsyncIterable<ChatEvent>) {
  const send = vi.fn(stream ?? (async function* () { yield { type: "status", state: "completed", taskId: "task-2" } as ChatEvent; }));
  const cancel = vi.fn(async () => {});
  const client: ChatClient = { protocolVersion: "test", history: read, send, cancel };
  setChatClientFactory(() => client);
  return { send, cancel };
}
afterEach(() => { resetChatClient(); vi.useRealTimers(); });

describe("persisted turn recovery", () => {
  it("restores a working turn on reopen, blocks admission and polls its final reply", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let history = working;
    const { send } = install(async () => history);
    const { result } = renderHook(() => useChat(SESSION));
    expect(result.current.isCheckingTask).toBe(true);
    await waitFor(() => expect(result.current.turnState).toBe("working"));
    await act(async () => { expect(await result.current.send("Next prompt")).toBe(false); });
    expect(send).not.toHaveBeenCalled();
    expect(result.current.sessionNotice).toMatch(/session busy/i);
    history = completed;
    await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
    expect(result.current.phase).toBe("idle");
    expect(result.current.turnState).toBe("completed");
    expect(result.current.messages).toEqual(completed.messages);
    expect(result.current.sessionNotice).toBeUndefined();
  });

  it.each(["clean close", "network error"])("observes backend work after a %s, without resending", async (ending) => {
    let history: ChatHistory = { messages: [] };
    const { send } = install(async () => history, async function* () {
      history = working;
      yield { type: "status", state: "working", taskId: "task-1" };
      if (ending === "network error") throw new ApiError("connection lost", { kind: "network", url: "send" });
    });
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => { await result.current.send("Run tests"); });
    expect(result.current.turnState).toBe("working");
    expect(result.current.turnError).toBeUndefined();
    expect(send).toHaveBeenCalledTimes(1);
    history = completed;
    await act(async () => { await result.current.refreshTranscript(); });
    expect(result.current.messages).toEqual(expect.arrayContaining(completed.messages));
    expect(result.current.phase).toBe("idle");
  });

  it("checks admission again when another tab starts work after this tab loaded", async () => {
    let history: ChatHistory = { messages: [] };
    const { send } = install(async () => history);
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    history = working;
    await act(async () => { expect(await result.current.send("Race")).toBe(false); });
    expect(send).not.toHaveBeenCalled();
    expect(result.current.turnState).toBe("working");
  });

  it("recovers a raced conflict as session busy, without a failed agent or phantom prompt", async () => {
    let history: ChatHistory = { messages: [] };
    const { send } = install(async () => history, async function* () {
      history = working;
      yield { type: "error", error: new ApiError("resource conflict", { kind: "http", status: 409, code: "Aborted", url: "send" }) };
    });
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => { expect(await result.current.send("Race")).toBe(false); });
    expect(result.current.turnError).toBeUndefined();
    expect(result.current.messages).toEqual([]);
    expect(result.current.turnState).toBe("working");
    expect(result.current.sessionNotice).toMatch(/session busy/i);
    expect(send).toHaveBeenCalledTimes(1);
  });

  it("keeps admission closed when recovery fails and retries on the next poll", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let down = false;
    const { send } = install(async () => {
      if (down) throw new Error("offline");
      return working;
    });
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.turnState).toBe("working"));
    down = true;
    await act(async () => { await result.current.refreshTranscript(); });
    expect(result.current.isCheckingTask).toBe(true);
    expect(result.current.turnState).toBe("working");
    expect(result.current.sessionNotice).toMatch(/could not confirm/i);
    down = false;
    await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
    expect(result.current.isCheckingTask).toBe(false);
    expect(send).not.toHaveBeenCalled();
  });

  it("lets a pending question resume its own task after reopening", async () => {
    const history: ChatHistory = { messages: [], turn: { taskId: "task-1", state: "input_required" }, awaitingReply: { kind: "unknown", taskId: "task-1" } };
    const { send } = install(async () => history);
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.pendingQuestion).toBeDefined());
    expect(result.current.phase).toBe("idle");
    await act(async () => { await result.current.send("Approved"); });
    expect(send).toHaveBeenCalledWith(expect.objectContaining({ taskId: "task-1" }));
  });

  it("does not submit a stale answer as a new prompt when another tab answered first", async () => {
    let history: ChatHistory = { messages: [], turn: { taskId: "task-1", state: "input_required" }, awaitingReply: { kind: "unknown", taskId: "task-1" } };
    const { send } = install(async () => history);
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.pendingQuestion).toBeDefined());
    history = completed;
    await act(async () => { expect(await result.current.send("Old answer")).toBe(false); });
    expect(send).not.toHaveBeenCalled();
    expect(result.current.pendingQuestion).toBeUndefined();
    expect(result.current.sessionNotice).toMatch(/question changed/i);
  });

  it.each(["failed", "canceled"] as const)("restores a persisted %s outcome", async (state) => {
    const error = state === "failed" ? new Error("tool failed") : undefined;
    install(async () => ({ messages: [], turn: { taskId: "task-1", state, error } }));
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.turnState).toBe(state));
    expect(result.current.phase).toBe("idle");
    expect(result.current.turnError).toBe(error);
    expect(result.current.canRetry).toBe(false);
  });

  it("cancels the recovered task rather than requiring a local stream", async () => {
    let history = working;
    const { cancel } = install(async () => history);
    cancel.mockImplementation(async () => { history = { messages: [], turn: { taskId: "task-1", state: "canceled" } }; });
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.turnState).toBe("working"));
    await act(async () => { await result.current.cancel(); });
    expect(cancel).toHaveBeenCalledWith(SESSION, "task-1");
    expect(result.current.turnState).toBe("canceled");
    expect(result.current.isCheckingTask).toBe(false);
  });

  it("serializes preflights so rapid sends cannot submit twice", async () => {
    let release!: (history: ChatHistory) => void;
    let reads = 0;
    const { send } = install(async () => ++reads === 1 ? { messages: [] } : new Promise((resolve) => { release = resolve; }));
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => {
      const first = result.current.send("First");
      expect(await result.current.send("Second")).toBe(false);
      release({ messages: [] });
      await first;
    });
    expect(send).toHaveBeenCalledTimes(1);
  });
  it("ignores a slower poll that finishes after a newer terminal-state read", async () => {
    let release!: (history: ChatHistory) => void;
    let reads = 0;
    install(async () => ++reads === 2 ? new Promise((resolve) => { release = resolve; }) : completed);
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => {
      const slow = result.current.refreshTranscript();
      await result.current.refreshTranscript();
      release(working);
      await slow;
    });
    expect(result.current.turnState).toBe("completed");
  });

  it("unlocks a failed preparation so the next preflight can proceed", async () => {
    const { send } = install(async () => ({ messages: [] }));
    const prepare = vi.fn().mockRejectedValueOnce(new Error("resume failed"));
    const { result } = renderHook(() => useChat(SESSION, prepare));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => { expect(await result.current.send("First")).toBe(false); });
    expect(result.current.isCheckingTask).toBe(true);
    await act(async () => { expect(await result.current.send("Second")).toBe(true); });
    expect(send).toHaveBeenCalledTimes(1);
  });

  it("leaves another session untouched when a recovery read lands after navigation", async () => {
    let release!: (history: ChatHistory) => void;
    let reads = 0;
    install(async () => ++reads === 2 ? new Promise((resolve) => { release = resolve; }) : { messages: [] });
    const { result, rerender } = renderHook(({ id }) => useChat({ ...SESSION, id }), { initialProps: { id: "session-1" } });
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    let slow!: Promise<void>;
    await act(async () => { slow = result.current.refreshTranscript(); });
    rerender({ id: "session-2" });
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => { release(working); await slow; });
    expect(result.current.turnState).toBe("idle");
    expect(result.current.messages).toEqual([]);
  });

  it("does not disguise a resource error from an accepted task as an admission conflict", async () => {
    const error = new ApiError("tool conflict", { kind: "http", status: 409, url: "send" });
    install(async () => ({ messages: [] }), async function* () {
      yield { type: "status", state: "working", taskId: "task-1" };
      yield { type: "error", error };
    });
    const { result } = renderHook(() => useChat(SESSION));
    await waitFor(() => expect(result.current.isCheckingTask).toBe(false));
    await act(async () => { expect(await result.current.send("Run tool")).toBe(true); });
    expect(result.current.turnError).toBe(error);
    expect(result.current.canRetry).toBe(true);
    expect(result.current.messages).toHaveLength(1);
  });

});
