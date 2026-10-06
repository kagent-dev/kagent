import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it } from "vitest";
import type { AgentInstance } from "@/api";
import { resetChatClient, setChatClientFactory } from "../chat";
import type { ChatClient, ChatMessage } from "../chat/types";
import { useConversationTitles } from "./useConversationTitles";

/** An unnamed conversation, as the rail lists it. */

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

/** One cache per test, shared by every mount in it, as the app's pages share one. */
function sharedCache() {
  const cache = new Map();
  return function Wrapper({ children }: { children: ReactNode }) {
    return <SWRConfig value={{ provider: () => cache }}>{children}</SWRConfig>;
  };
}

/** The conversation on screen, as the rail passes it: its title only once one exists. */
type Open = { id: string; title?: string };

afterEach(() => resetChatClient());

describe("useConversationTitles", () => {
  it("reads a conversation's title once, not again when the list grows", async () => {
    const histories = new Map([["named", [said("An older question")]]]);
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));

    const { result, rerender } = renderHook(
      ({ listed }) => useConversationTitles(listed),
      {
        wrapper: sharedCache(),
        initialProps: { listed: [conversation("named"), conversation("fresh")] },
      },
    );
    await waitFor(() => expect(result.current).toEqual({ named: "An older question" }));

    // A new set of ids is a new read, but only of the rows that have no title yet.
    reads.length = 0;
    rerender({ listed: [conversation("named"), conversation("fresh"), conversation("third")] });
    await waitFor(() => expect(reads.sort()).toEqual(["fresh", "third"]));
    expect(result.current).toEqual({ named: "An older question" });
  });

  // The open row is titled from its transcript; the rail's read of it came before that.
  it("keeps the open conversation's title once another is opened", async () => {
    const histories = new Map<string, ChatMessage[]>();
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));
    const wrapper = sharedCache();
    const listed = [conversation("fresh"), conversation("other")];

    const { result, rerender } = renderHook<Record<string, string>, { open: Open }>(
      ({ open }) => useConversationTitles(listed, open),
      { wrapper, initialProps: { open: { id: "fresh", title: "This is twin B." } } },
    );
    await waitFor(() => expect(reads.sort()).toEqual(["fresh", "other"]));

    rerender({ open: { id: "other" } });
    expect(result.current).toEqual({ fresh: "This is twin B." });

    // And on the next page, whose rail is a new mount reading the same cache.
    const detailsPage = renderHook(() => useConversationTitles(listed), { wrapper });
    expect(detailsPage.result.current).toEqual({ fresh: "This is twin B." });
  });

  it("takes the title back when the transcript takes back a refused first message", async () => {
    const histories = new Map<string, ChatMessage[]>();
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));
    const listed = [conversation("fresh"), conversation("other")];

    const { result, rerender } = renderHook<Record<string, string>, { open: Open }>(
      ({ open }) => useConversationTitles(listed, open),
      {
        wrapper: sharedCache(),
        initialProps: { open: { id: "fresh", title: "This is twin B." } },
      },
    );
    await waitFor(() => expect(reads.sort()).toEqual(["fresh", "other"]));

    rerender({ open: { id: "fresh" } });
    rerender({ open: { id: "other" } });
    expect(result.current).toEqual({});
  });

  it("takes the title back even after a read that ran while it was remembered", async () => {
    const histories = new Map<string, ChatMessage[]>();
    const reads: string[] = [];
    setChatClientFactory(() => client(histories, reads));
    const two = [conversation("fresh"), conversation("other")];
    const three = [...two, conversation("third")];

    const { result, rerender } = renderHook<
      Record<string, string>,
      { listed: AgentInstance[]; open: Open }
    >(({ listed, open }) => useConversationTitles(listed, open), {
      wrapper: sharedCache(),
      initialProps: { listed: two, open: { id: "fresh", title: "This is twin B." } },
    });
    await waitFor(() => expect(reads.sort()).toEqual(["fresh", "other"]));

    // The list grows while the title is remembered, so a new read is cached.
    reads.length = 0;
    rerender({ listed: three, open: { id: "fresh", title: "This is twin B." } });
    await waitFor(() => expect(reads.sort()).toEqual(["other", "third"]));

    rerender({ listed: three, open: { id: "fresh" } });
    rerender({ listed: three, open: { id: "other" } });
    expect(result.current).toEqual({});
  });

  // Each page's rail is its own read; one cached before the first message is not final.
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
