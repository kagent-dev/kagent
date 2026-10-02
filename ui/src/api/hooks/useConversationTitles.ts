import { useEffect, useRef } from "react";
import useSWR, { useSWRConfig } from "swr";
import { getChatClient } from "@/api/chat";
import { autoTitleFrom } from "@/components/agent-instances/instanceLabels";
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
 * Display only, exactly as on the chat page: nothing is written back. A stored
 * auto-title would be a name nobody chose, indistinguishable from one somebody did.
 *
 * Failures are silent and per-conversation. A title is a convenience over an id that
 * already identifies the row, so a conversation whose read fails keeps its id rather
 * than turning a cosmetic problem into an error the reader must act on.
 *
 * A conversation can be listed before anything has been said in it: the new-chat page
 * creates it and refreshes the list, and only the chat page it navigates to sends the
 * first message. So a read that found no title is not final. The open conversation's
 * title, which the page derives from its own transcript, is remembered as that
 * conversation's, so leaving it — mid-reply or not — does not lose it. Past that,
 * every mount whose set has a row still untitled re-reads on arrival. A title found
 * once is final, so a re-read costs a request only per row still untitled.
 */
export function useConversationTitles(
  instances: readonly AgentInstance[] | undefined,
  open?: { id: string; title?: string },
): Record<string, string> {
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

  const derived = derivedTitles(useSWRConfig().cache);

  /*
   * The open conversation's title, from the transcript on screen.
   *
   * Taken back if the transcript takes it back: a first message the server refuses
   * outright is removed from it, and a title naming a message nothing kept would last
   * only until the next reload. Only a title remembered here is taken back, and only
   * while its conversation is still the open one — one derived from a read is final.
   */
  const openId = open?.id;
  const openTitle = open?.title;
  const remembered = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (remembered.current !== openId) remembered.current = undefined;
    if (!openId) return;
    if (openTitle) {
      if (derived.has(openId)) return;
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
        targets.map(async (instance) => {
          const known = derived.get(instance.id);
          if (known) return [instance.id, known] as const;
          try {
            if (!instance.agent) return undefined;
            const history = await getChatClient().history({

              id: instance.id,
              agent: instance.agent,
            });
            const said = history.messages
              .find((message) => message.role === "user")
              ?.parts.find((part) => part.kind === "text")?.text;
            const title = autoTitleFrom(said);
            if (!title) return undefined;
            derived.set(instance.id, title);
            return [instance.id, title] as const;
          } catch {
            // Deliberately quiet — see this hook's note on failures.
            return undefined;
          }
        }),
      );
      return Object.fromEntries(entries.filter((entry) => entry !== undefined));
    },
    {
      // A conversation's first message never changes, so a derived title cannot go
      // stale. Re-reading on focus would spend a request per row for a value that is
      // the same every time.
      revalidateOnFocus: false,
      // A cached read can predate a conversation's first message, so it is trusted
      // only when every row in it already has a title.
      revalidateIfStale: targets.some((instance) => !derived.has(instance.id)),
      keepPreviousData: true,
    },
  );

  /*
   * From `derived`, not from `data` alone: the cached read for this set of ids can
   * predate a title found since under another set. `data` is still read, because a
   * read landing is what re-renders this.
   */
  const titles: Record<string, string> = { ...data };
  for (const instance of targets) {
    const title = derived.get(instance.id);
    if (title) titles[instance.id] = title;
  }
  return titles;
}

/**
 * Titles already derived, shared by every mount reading through one SWR cache.
 *
 * Per conversation rather than per set of ids, because that is what a title belongs
 * to — the rail on each page lists a different set, and a title found under one is
 * as true under the next. Held against the cache so an isolated cache, as in a test,
 * starts with none.
 */
const derivedByCache = new WeakMap<object, Map<string, string>>();

function derivedTitles(cache: object): Map<string, string> {
  let titles = derivedByCache.get(cache);
  if (!titles) {
    titles = new Map();
    derivedByCache.set(cache, titles);
  }
  return titles;
}
