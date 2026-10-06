import { useEffect, useRef } from "react";
import useSWR, { useSWRConfig } from "swr";
import { getChatClient } from "@/api/chat";
import { autoTitleFrom } from "@/components/agent-instances/instanceLabels";
import { messageSummary } from "@/components/chat/messageText";
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
 * A conversation can be listed before its first message, so an untitled row is re-read
 * on mount, and the open conversation keeps its transcript's title once it is left.
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
        targets.map(async (instance) => {
          if (derived.has(instance.id)) return undefined;
          try {
            if (!instance.agent) return undefined;
            const history = await getChatClient().history({

              id: instance.id,
              agent: instance.agent,
            });
            const first = history.messages.find((message) => message.role === "user");
            const title = autoTitleFrom(first && messageSummary(first));
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
      // Re-read on mount while a row is untitled; a read can predate a first message.
      revalidateIfStale: targets.some((instance) => !derived.has(instance.id)),
      keepPreviousData: true,
    },
  );

  // Titles come from `derived`; `data` is read so that a landed read re-renders this.
  const titles: Record<string, string> = { ...data };
  for (const instance of targets) {
    const title = derived.get(instance.id);
    if (title) titles[instance.id] = title;
  }
  return titles;
}

/** Titles per conversation, shared by every mount on one SWR cache. */
const derivedByCache = new WeakMap<object, Map<string, string>>();

function derivedTitles(cache: object): Map<string, string> {
  let titles = derivedByCache.get(cache);
  if (!titles) {
    titles = new Map();
    derivedByCache.set(cache, titles);
  }
  return titles;
}
