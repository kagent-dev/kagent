import useSWR from "swr";
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
 */
export function useConversationTitles(
  instances: readonly AgentInstance[] | undefined,
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
    ? ["conversation-titles", targets.map((t) => `${t.namespace}/${t.id}`).sort().join(",")]
    : null;

  const { data } = useSWR(
    key,
    async () => {
      const entries = await Promise.all(
        targets.map(async (instance) => {
          try {
            const history = await getChatClient().history({
              namespace: instance.namespace,
              id: instance.id,
            });
            const said = history.messages
              .find((message) => message.role === "user")
              ?.parts.find((part) => part.kind === "text")?.text;
            const title = autoTitleFrom(said);
            return title ? ([instance.id, title] as const) : undefined;
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
      revalidateIfStale: false,
      keepPreviousData: true,
    },
  );

  return data ?? {};
}
