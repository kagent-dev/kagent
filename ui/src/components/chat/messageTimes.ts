import type { ChatMessage } from "@/api";

/** How long one sender can go quiet and still be in the same run. */
export const RUN_GAP_MS = 5 * 60 * 1000;

/** Where a message sits in time, as the transcript draws it. */
export interface MessageTime {
  /** When it was sent, or undefined when the timestamp is missing or unreadable. */
  at?: Date;
  /** A day label to draw above it, when it opens a new day. */
  dayLabel?: string;
  /** Whether its time shows at rest, rather than only on hover. */
  showTime: boolean;
}

/** The timestamp as a date, or undefined rather than an invalid date. */
export function parseMessageTime(value: string | undefined): Date | undefined {
  if (!value) return undefined;
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? undefined : at;
}

/** The local calendar day, for comparing two dates by day. */
export function dayKey(at: Date): string {
  return `${at.getFullYear()}-${at.getMonth()}-${at.getDate()}`;
}

/** "Today", "Yesterday", "Mon, Nov 17", or "Nov 3, 2025" outside this year. */
export function dayLabel(at: Date, now: Date, locale?: string): string {
  if (dayKey(at) === dayKey(now)) return "Today";
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  if (dayKey(at) === dayKey(yesterday)) return "Yesterday";
  const options: Intl.DateTimeFormatOptions =
    at.getFullYear() === now.getFullYear()
      ? { weekday: "short", month: "short", day: "numeric" }
      : { month: "short", day: "numeric", year: "numeric" };
  return new Intl.DateTimeFormat(locale, options).format(at);
}

/** The time of day, such as "2:41 PM". */
export function timeLabel(at: Date, locale?: string): string {
  return new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit" }).format(at);
}

/** The full date and time, for the tooltip. */
export function fullTimeLabel(at: Date, locale?: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "full", timeStyle: "short" }).format(at);
}

/**
 * Day breaks and resting times for a transcript in order. A time shows at rest on the
 * first message of a run: same sender, same day, each within `RUN_GAP_MS` of the last.
 */
export function planMessageTimes(
  messages: readonly Pick<ChatMessage, "id" | "role" | "createdAt">[],
  now: Date,
  locale?: string,
): Map<string, MessageTime> {
  const plan = new Map<string, MessageTime>();
  let lastDay: string | undefined;
  let last: { role: string; at: Date } | undefined;
  for (const message of messages) {
    const at = parseMessageTime(message.createdAt);
    if (!at) {
      plan.set(message.id, { showTime: false });
      continue;
    }
    const day = dayKey(at);
    const newDay = day !== lastDay;
    const sameRun =
      !newDay &&
      last !== undefined &&
      last.role === message.role &&
      Math.abs(at.getTime() - last.at.getTime()) <= RUN_GAP_MS;
    plan.set(message.id, {
      at,
      dayLabel: newDay ? dayLabel(at, now, locale) : undefined,
      showTime: !sameRun,
    });
    lastDay = day;
    last = { role: message.role, at };
  }
  return plan;
}
