import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { resetChatClient, setChatClientFactory } from "../chat";
import type { ChatClient, ChatEvent } from "../chat/types";
import { RUNTIME_CONFIG_DEFAULTS } from "../runtimeConfig";
import { useChat } from "./useChat";

/**
 * A stream that starts and then goes silent — the case the deployment's stream
 * timeout exists for. Without a timeout the turn spins forever, which looks to
 * the user exactly like an agent still thinking.
 */
function stallingClient(onAbort: () => void): ChatClient {
  return {
    protocolVersion: "test",
    history: async () => ({ messages: [] }),
    cancel: async () => {},
    send: ({ signal }) =>
      (async function* (): AsyncIterable<ChatEvent> {
        yield { type: "status", state: "working", taskId: "task-1" };
        // Never emits again. Resolves only when the turn is aborted, which is
        // what the timeout is expected to do.
        await new Promise<void>((resolve) => {
          signal?.addEventListener("abort", () => {
            onAbort();
            resolve();
          });
        });
      })(),
  };
}

describe("useChat stream timeout", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    resetChatClient();
  });

  it("fails the turn when the stream goes quiet, and aborts the request", async () => {
    let aborted = false;
    setChatClientFactory(() => stallingClient(() => (aborted = true)));

    const { result } = renderHook(() => useChat({ namespace: "kagent", id: "instance-1" }));

    await act(async () => {
      void result.current.send("why is checkout crashlooping?");
    });

    // Mid-window: still streaming, because a slow answer is not a failed one.
    await act(async () => {
      vi.advanceTimersByTime(RUNTIME_CONFIG_DEFAULTS.streamTimeoutMs / 2);
    });
    expect(result.current.turnError).toBeUndefined();

    await act(async () => {
      vi.advanceTimersByTime(RUNTIME_CONFIG_DEFAULTS.streamTimeoutMs);
    });

    await waitFor(() => {
      expect(result.current.turnState).toBe("failed");
    });
    expect(result.current.turnError?.message).toMatch(/stopped responding/i);
    // The point of aborting is that the request is released, not merely that the
    // UI stops waiting on it.
    expect(aborted).toBe(true);
  });
});
