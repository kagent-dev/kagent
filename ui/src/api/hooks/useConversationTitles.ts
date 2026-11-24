import { useEffect, useMemo, useRef } from "react";
import useSWR, { useSWRConfig } from "swr";
import { getConversationClient } from "@/api/conversation";
import { autoTitleFrom } from "@/components/agent-instances/instanceLabels";
import { messageSummary } from "@/components/conversation/messageText";
import type { AgentInstance } from "@/api";

/**
 * How many conversations are worth a read.
 *
 * Each title costs one `ListTasks`, so this is a real budget rather than a formality.
 * A rail showing more than this many is a rail nobody is reading top to bottom, and
 * the rest keep the id they always had — which is honest, where a spinner on forty
 * rows would not be.
 */
const TITLE_BUDGET = 30;

/**
 * A title for each conversation, derived from what was first said in it.
 *
 * The rail used to show `Untitled · 50b46891` for every conversation except the open
 * one, because only the page rendering a transcript had the transcript to derive a
 * title from. That made the list very nearly unusable: the one row a reader could
 * identify was the one they were already looking at.
 *
 * It is possible now for a reason worth recording. Deriving a title needs the task
 * list, which the A2A gateway refused for any conversation that was not ready — and
 * with conversations giving their workers back after every turn, that is most of
 * them. The gateway now answers a task read from the store whatever state the
 * instance is in, because that is where the transcript lives.
 *
 * Display only, exactly as on the conversation page: nothing is written back. A stored
 * auto-title would be a name nobody chose, indistinguishable from one somebody did.
 *
 * Failures are silent and per-conversation. A title is a convenience over an id that
 * already identifies the row, so a conversation whose read fails keeps its id rather
 * than turning a cosmetic problem into an error the reader must act on.
 *
 * A conversation can be listed before its first message, so an untitled row is re-read
 * on mount, and the open conversation keeps its transcript's title once it is left.
 */
export type ConversationTitles = {
  /** Derived titles by conversation id. */
  titles: Readonly<Record<string, string>>;
  /** Ids whose title read is in flight. Rows past the budget are never read, so never here. */
  loading: ReadonlySet<string>;
};

export function useConversationTitles(
  instances: readonly AgentInstance[] | undefined,
  open?: { id: string; title?: string },
): ConversationTitles {
  /*
   * Keyed by the ids themselves, so the read repeats when the set changes and not
   * when the array's identity does — a list re-read on a timer hands back a new array
   * of equal rows every time, which as a key would re-fetch every title on every poll.
   */
  const targets = (instances ?? [])
    .filter((instance) => instance.name.trim() === "")
    .slice(0, TITLE_BUDGET);
  const key = targets.length > 0
    ? ["conversation-titles", targets.map((t) => t.id).sort().join(",")]
    : null;

  const { titles: derived, settled } = titleStore(useSWRConfig().cache);

  // Open conversation's title, kept after leaving; dropped if its first message is refused.
  const openId = open?.id;
  const openTitle = open?.title;
  const remembered = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (remembered.current !== openId) remembered.current = undefined;
    if (!openId) return;
    if (openTitle) {
      if (derived.get(openId) === openTitle) return;
      derived.set(openId, openTitle);
      remembered.current = openId;
    } else if (remembered.current === openId) {
      derived.delete(openId);
      remembered.current = undefined;
    }
  }, [derived, openId, openTitle]);

  const { data } = useSWR(
    key,
    async () => {
      const entries = await Promise.all(
        // `null` is a settled read with no title, which is what lets a row stop loading.
        targets.map(async (instance): Promise<readonly [string, string | null]> => {
          // Already known: settled, with the title left in `derived` so it can be taken back.
          if (derived.has(instance.id)) return [instance.id, null];
          try {
            if (!instance.agent) return [instance.id, null];
            const history = await getConversationClient().history({
              id: instance.id,
              agent: instance.agent,
            });
            const first = history.messages.find((message) => message.role === "user");
            const title = autoTitleFrom(first && messageSummary(first));
            if (!title) return [instance.id, null];
            derived.set(instance.id, title);
            return [instance.id, title];
          } catch {
            // Deliberately quiet — see this hook's note on failures.
            return [instance.id, null];
          }
        }),
      );
      for (const [id] of entries) settled.add(id);
      return Object.fromEntries(entries) as Record<string, string | null>;
    },
    {
      // A conversation's first message never changes, so a derived title cannot go
      // stale. Re-reading on focus would spend a request per row for a value that is
      // the same every time.
      revalidateOnFocus: false,
      // Re-read on mount while a row is untitled; a read can predate a first message.
      revalidateIfStale: targets.some((instance) => !derived.has(instance.id)),
      keepPreviousData: true,
    },
  );

  // A row loads only until its first read settles, under any key or mount; re-reads update in place.
  const titles: Record<string, string> = {};
  const loading: string[] = [];
  for (const instance of targets) {
    const title = derived.get(instance.id) ?? data?.[instance.id];
    if (title) titles[instance.id] = title;
    else if (!settled.has(instance.id) && !(data && instance.id in data)) loading.push(instance.id);
  }
  // Same object until something changes, so callers can memoise on it.
  const signature = JSON.stringify([titles, loading]);
  return useMemo(() => {
    const [t, l] = JSON.parse(signature) as [Record<string, string>, string[]];
    return { titles: t, loading: new Set(l) };
  }, [signature]);
}

/** Titles, and ids whose read has settled, shared by every mount on one SWR cache. */
interface TitleStore {
  titles: Map<string, string>;
  settled: Set<string>;
}

const storeByCache = new WeakMap<object, TitleStore>();

function titleStore(cache: object): TitleStore {
  let store = storeByCache.get(cache);
  if (!store) {
    store = { titles: new Map(), settled: new Set() };
    storeByCache.set(cache, store);
  }
  return store;
}
