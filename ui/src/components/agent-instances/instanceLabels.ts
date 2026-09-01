/**
 * How an instance's state and operation read on screen.
 *
 * Kept apart from the components that render them, and out of both pages, for one
 * reason: the list and the detail view must not be able to disagree about what a
 * state is called. They did, in an earlier draft — the table said "Failed" while
 * the detail page said "Error" for the same enum value — and a reader moving
 * between the two has no way to tell whether they are looking at the same thing.
 *
 * Everything here is a pure function of the domain type, which also makes it the
 * part of this feature worth unit testing: the wording is the behaviour.
 */

import { formatDistanceToNow } from "date-fns";
import type {
  AgentInstance,
  AgentInstanceOperation,
  AgentInstanceState,
} from "@/api";

/** antd `Tag` colours. Named rather than inlined so the two pages cannot drift. */
export type TagTone = "success" | "processing" | "warning" | "error" | "default";

export interface StateAppearance {
  /** What the tag says. */
  label: string;
  tone: TagTone;
  /** One sentence a reader can act on, shown beside the tag on the detail page. */
  meaning: string;
}

/**
 * Every `AgentInstanceState`, in words.
 *
 * `unspecified` is not folded into anything friendlier. Proto3 gives the enum a
 * zero value and the controller can leave it there, so "the cluster did not say"
 * is a real answer and the only honest one — rendering it as "Unknown" alongside
 * the genuinely-unrecognised case would merge two different facts, and rendering
 * it as "Pending" would invent a third.
 */
const STATES: Record<AgentInstanceState, StateAppearance> = {
  ready: {
    label: "Ready",
    tone: "success",
    meaning: "The instance is running and can be reached over A2A.",
  },
  creating: {
    label: "Creating",
    tone: "processing",
    meaning: "The controller is still bringing this instance up.",
  },
  suspended: {
    label: "Suspended",
    tone: "default",
    meaning:
      "The instance is stopped but its state is kept. Resuming brings it back.",
  },
  failed: {
    label: "Failed",
    tone: "error",
    meaning: "The controller could not bring this instance to its intended state.",
  },
  deleting: {
    label: "Deleting",
    tone: "warning",
    meaning: "The instance is being torn down. It will stop being listed.",
  },
  deleted: {
    label: "Deleted",
    tone: "default",
    meaning: "The instance is gone. Only its record remains.",
  },
  unspecified: {
    label: "Not reported",
    tone: "default",
    meaning:
      "The controller sent no state for this instance. That is the record as it stands, not a value this page failed to read.",
  },
  unknown: {
    label: "Unrecognised",
    tone: "warning",
    meaning:
      "The controller reported a state this build of the UI does not know about, which usually means the cluster is newer than this page.",
  },
};

export function stateAppearance(state: AgentInstanceState): StateAppearance {
  return STATES[state];
}

export interface OperationAppearance {
  label: string;
  tone: TagTone;
  /** False when nothing is in flight, so a caller can render it differently. */
  inProgress: boolean;
}

/**
 * Every `AgentInstanceOperation`, in words.
 *
 * The zero value means something different here than it does for a state: the
 * controller clears `operation` when it finishes, so nothing in flight is the
 * ordinary condition rather than a gap in the record. `inProgress` carries that
 * distinction so a caller does not have to compare against a magic string.
 */
const OPERATIONS: Record<AgentInstanceOperation, OperationAppearance> = {
  unspecified: { label: "None in progress", tone: "default", inProgress: false },
  create: { label: "Creating", tone: "processing", inProgress: true },
  suspend: { label: "Suspending", tone: "processing", inProgress: true },
  resume: { label: "Resuming", tone: "processing", inProgress: true },
  delete: { label: "Deleting", tone: "warning", inProgress: true },
  unknown: { label: "Unrecognised operation", tone: "warning", inProgress: true },
};

export function operationAppearance(
  operation: AgentInstanceOperation,
): OperationAppearance {
  return OPERATIONS[operation];
}

/**
 * The one wording used everywhere a value the API did not send would go.
 *
 * A blank cell and an em dash both read as "this page has nothing to say", which is
 * indistinguishable from a bug. Saying it was not reported puts the absence where
 * it belongs — on the record, not on the reader.
 */
export const NOT_REPORTED = "Not reported";

/** A value, or the standard wording when the API did not send one. */
export function orNotReported(value: string | undefined): string {
  return value && value.trim() !== "" ? value : NOT_REPORTED;
}

/**
 * "3 days ago", from an RFC3339 timestamp.
 *
 * An unset timestamp arrives as an empty string — proto3 has no absent one — and an
 * unparseable one has been seen from a hand-written fixture. Both answer with the
 * standard wording rather than "Invalid Date", which is the string this used to put
 * in the age column.
 */
export function relativeAge(timestamp: string): string {
  if (!timestamp) return NOT_REPORTED;
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) return NOT_REPORTED;
  return `${formatDistanceToNow(parsed)} ago`;
}

/**
 * The short identifier a table shows for an instance.
 *
 * Eight characters is enough to tell the rows of one namespace apart while leaving
 * room for the columns that carry meaning. The full id is on the detail page, where
 * it can be copied.
 */
export function shortInstanceId(id: string): string {
  return id.slice(0, 8);
}

/** How long an auto-derived title may be before it is cut. */
const AUTO_TITLE_LENGTH = 60;

/**
 * A conversation's title, derived from the first thing said in it.
 *
 * Free where the transcript has already been read, and nowhere else — which is the
 * whole of why this is a separate function rather than something `conversationTitle`
 * does for itself. Deriving it needs `ListTasks` for that one conversation, so a
 * *list* of conversations would pay one round trip per row to put a title on each;
 * the chat page has the messages in hand already, and passes what it found.
 *
 * Cut on a word boundary where there is one within reach, because a title broken
 * mid-word reads as a rendering fault rather than as a summary. Returns `undefined`
 * for anything with no words in it, so a caller falls back rather than showing an
 * ellipsis on its own.
 */
export function autoTitleFrom(firstMessage: string | undefined): string | undefined {
  const text = (firstMessage ?? "").replace(/\s+/g, " ").trim();
  if (text === "") return undefined;
  if (text.length <= AUTO_TITLE_LENGTH) return text;

  const cut = text.slice(0, AUTO_TITLE_LENGTH);
  const lastSpace = cut.lastIndexOf(" ");
  // Only if the break is late enough to still be most of the title; a single long
  // word would otherwise be cut down to almost nothing.
  const stem = lastSpace > AUTO_TITLE_LENGTH / 2 ? cut.slice(0, lastSpace) : cut;
  return `${stem.trimEnd()}…`;
}

/**
 * What to call a conversation on screen.
 *
 * Three answers in order of how much they were meant: the name the reader gave it, a
 * title derived from its first message where the caller already had the transcript,
 * and — failing both — a plain statement that it is untitled, with enough of the id
 * to tell it from its neighbours.
 *
 * **A bare UUID is never one of the three.** A row reading
 * `6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44` under a "Name" heading is a page presenting
 * a database key as if somebody had chosen it, and it was the thing that made the
 * old agents list unreadable: eight rows of hex, no two distinguishable at a glance.
 * Saying "Untitled" says the true thing — nobody has named this yet — and the short
 * id beside it is plainly an identifier rather than a name.
 */
export function conversationTitle(
  instance: AgentInstance,
  autoTitle?: string,
): string {
  if (instance.name.trim() !== "") return instance.name;
  if (autoTitle && autoTitle.trim() !== "") return autoTitle;
  return `Untitled · ${shortInstanceId(instance.id)}`;
}

/**
 * Whether what is on screen is a name somebody chose.
 *
 * Rendering is not the only caller: a rename box opens on the *stored* name, not on
 * whatever is being displayed in its place, or clearing a title would be impossible
 * — the box would come up pre-filled with the placeholder and saving it would turn
 * an honest "Untitled" into a literal one.
 */
export function hasConversationName(instance: AgentInstance): boolean {
  return instance.name.trim() !== "";
}

/** Labels as `key=value`, sorted, so the same set always reads the same way. */
export function labelPairs(instance: AgentInstance): string[] {
  return Object.entries(instance.labels)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => `${key}=${value}`);
}
