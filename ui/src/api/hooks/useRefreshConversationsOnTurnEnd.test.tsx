import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import useSWR from "swr";
import { describe, expect, it } from "vitest";
import type { ChatPhase } from "./useChat";
import { useRefreshConversationsOnTurnEnd } from "./useRefreshConversationsOnTurnEnd";

function setup() {
  const reads = { list: 0, one: 0, other: 0 };
  const wrapper = ({ children }: { children: ReactNode }) => (
    <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>{children}</SWRConfig>
  );
  const view = renderHook(
    ({ phase }: { phase: ChatPhase }) => {
      useSWR(["agentInstances.list", false], async () => ++reads.list, {
        revalidateOnFocus: false,
      });
      useSWR(["agentInstances.get", "a"], async () => ++reads.one, { revalidateOnFocus: false });
      useSWR(["models.list"], async () => ++reads.other, { revalidateOnFocus: false });
      useRefreshConversationsOnTurnEnd(phase);
    },
    { wrapper, initialProps: { phase: "idle" as ChatPhase } },
  );
  return { reads, ...view };
}

describe("useRefreshConversationsOnTurnEnd", () => {
  it("re-reads the conversation reads once when a turn ends, and nothing else", async () => {
    const { reads, rerender } = setup();
    await waitFor(() => expect(reads).toEqual({ list: 1, one: 1, other: 1 }));

    rerender({ phase: "streaming" });
    rerender({ phase: "streaming" });
    expect(reads).toEqual({ list: 1, one: 1, other: 1 });

    rerender({ phase: "idle" });
    await waitFor(() => expect(reads).toEqual({ list: 2, one: 2, other: 1 }));

    rerender({ phase: "idle" });
    expect(reads).toEqual({ list: 2, one: 2, other: 1 });
  });
});
