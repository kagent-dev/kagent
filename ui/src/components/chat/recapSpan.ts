const MINUTE_MS = 60_000;

const plural = (count: number, unit: string) => (count === 1 ? unit : `${count} ${unit}s`);

/** How long the conversation ran up to the recap, as "the last 6 minutes". Empty when the times are unusable. */
export function recapSpan(startedAt: string | undefined, generatedAt: string): string {
  const ms = new Date(generatedAt).getTime() - new Date(startedAt ?? "").getTime();
  if (!Number.isFinite(ms) || ms < 0) return "";
  const minutes = Math.max(1, Math.round(ms / MINUTE_MS));
  if (minutes < 60) return `the last ${plural(minutes, "minute")}`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `the last ${plural(hours, "hour")}`;
  return `the last ${plural(Math.round(hours / 24), "day")}`;
}
