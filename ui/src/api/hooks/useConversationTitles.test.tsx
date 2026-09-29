import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it } from "vitest";
import type { AgentInstance } from "@/api";
import { resetChatClient, setChatClientFactory } from "../chat";
import type { ChatClient, ChatMessage } from "../chat/types";
import { useConversationTitles } from "./useConversationTitles";
import { useInvalidateConversationTitles } from "./useInvalidateConversationTitles";

/**
 * A conversation listed before its first message, the way the new-chat page lists it.
 *
 * The rail used to read its title once, find no message, and never read it again —
 * so the row showed `Untitled · <id>` as soon as the reader left it.
 */

function conversation(id: string): AgentInstance {
  return {
    id,
    name: "",
    creator: "",
    agent: "team-a/assistant",
    state: "ready",
    operation: "unspecified",
    createdAt: "",
    updatedAt: "",
  };
}

function said(text: string): ChatMessage {
  return { id: `${text}-msg`, role: "user", parts: [{ kind: "text", text }], createdAt: "" };
}

/** A transport whose histories the test fills in, counting the reads of each. */
function client(histories: Map<string, ChatMessage[]>, reads: string[]): ChatClient {
  return {
    protocolVersion: "test",
    history: async ({ id }) => {
      reads.push(id);
      return { messages: histories.get(id) ?? [] };
    },
    cancel: async () => {},
    send: () => (async function* () {})(),
  };
}

/**
 * A cache of the test's own, so no read leaks between tests — shared by every mount
 * within one, the way every page of the app shares one.
 */
function sharedCache() {
  const cache = new Map();
  return function Wrapper({ children }: { children: ReactNode }) {
    return <SWRConfig value={{ provider: () => cache }}>{children}</SWRConfig>;
  };
}

afterEach(() => resetChatClient());

describe("useConversationTitles", () => {
  it("titles a conversation once its first message exists and the titles are re-read", async () => {
    const histories = new Map([["named", [said("An older question")]]]);
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));
    const listed = [conversation("named"), conversation("fresh")];

    const { result } = renderHook(
      () => ({
        titles: useConversationTitles(listed),
        invalidate: useInvalidateConversationTitles(),
      }),
      { wrapper: sharedCache() },
    );

    await waitFor(() => expect(result.current.titles).toEqual({ named: "An older question" }));

    // The first turn of the new conversation lands, and the chat page goes idle.
    histories.set("fresh", [said("This is twin B.")]);
    reads.length = 0;
    await act(() => result.current.invalidate());

    await waitFor(() =>
      expect(result.current.titles).toEqual({
        named: "An older question",
        fresh: "This is twin B.",
      }),
    );
    // A title already derived is final, so only the untitled row was read again.
    expect(reads).toEqual(["fresh"]);
  });

  /*
   * Each page's rail lists its own set of conversations, and each set is its own read.
   * The one the new-chat page cached was made before anything had been said, so a page
   * that lists that set again must not take it as the last word.
   */
  it("re-reads a cached read that left a row untitled when another page mounts it", async () => {
    const histories = new Map<string, ChatMessage[]>();
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));
    const wrapper = sharedCache();
    const listed = [conversation("fresh")];

    const newChatPage = renderHook(() => useConversationTitles(listed), { wrapper });
    await waitFor(() => expect(reads).toEqual(["fresh"]));
    expect(newChatPage.result.current).toEqual({});
    newChatPage.unmount();

    histories.set("fresh", [said("This is twin B.")]);
    const detailsPage = renderHook(() => useConversationTitles(listed), { wrapper });

    await waitFor(() => expect(detailsPage.result.current).toEqual({ fresh: "This is twin B." }));
  });

  it("answers from a title found under another set of conversations, without reading", async () => {
    const histories = new Map<string, ChatMessage[]>();
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));
    const wrapper = sharedCache();

    const before = renderHook(() => useConversationTitles([conversation("fresh")]), { wrapper });
    await waitFor(() => expect(reads).toEqual(["fresh"]));
    before.unmount();

    // The chat page's rail lists more than the new-chat page's did, and finds the title.
    histories.set("fresh", [said("This is twin B.")]);
    const chatPage = renderHook(
      () => useConversationTitles([conversation("fresh"), conversation("other")]),
      { wrapper },
    );
    await waitFor(() => expect(chatPage.result.current).toEqual({ fresh: "This is twin B." }));
    chatPage.unmount();
    reads.length = 0;

    // Back on a page listing the first set, whose cached read still says nothing.
    const after = renderHook(() => useConversationTitles([conversation("fresh")]), { wrapper });
    expect(after.result.current).toEqual({ fresh: "This is twin B." });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(reads).toEqual([]);
  });
});
