import type { ScheduledRun } from "@/generated/kagent/api/v1/scheduled_runs_pb";
import type { FieldIssue } from "@/components/form/useSubmitValidation";
import { parseSchedule, type ScheduleTiming } from "./scheduleTiming";

export const NAME_MAX = 200;
export const CRON_MAX = 256;
export const TIME_ZONE_MAX = 253;
export const PROMPT_MAX_BYTES = 32768;
export const TIMEOUT_MAX_SECONDS = 9223372036;
export const DEFAULT_TIMEOUT_SECONDS = 900;

export interface ScheduleDraft extends Omit<ScheduleTiming, "minute" | "monthDay"> {
  agent: string;
  name: string;
  timeZone: string;
  prompt: string;
  enabled: boolean;
  continueConversation: boolean;
  /** Null while the field is empty. */
  timeoutSeconds: number | null;
  minute: number | null;
  monthDay: number | null;
}

/** A new schedule's defaults, or the values of the one being edited. */
export function draftFromSchedule(schedule?: ScheduledRun): ScheduleDraft {
  const config = schedule?.config;
  const timeout = config?.executionTimeout;
  return {
    ...parseSchedule(config?.schedule ?? "0 9 * * *"),
    agent: "",
    name: config?.name ?? "",
    timeZone: config?.timeZone || "UTC",
    prompt: config?.prompt ?? "",
    enabled: !(config?.paused ?? false),
    continueConversation: config?.continueConversation ?? false,
    timeoutSeconds: timeout ? Number(timeout.seconds) + timeout.nanos / 1e9 : DEFAULT_TIMEOUT_SECONDS,
  };
}

const isInteger = (value: number | null, min: number, max: number) =>
  value !== null && Number.isInteger(value) && value >= min && value <= max;

/** Every problem with a draft, filed under the field that shows it. */
export function scheduleIssues(draft: ScheduleDraft, options: { needsAgent: boolean }): FieldIssue[] {
  const issues: FieldIssue[] = [];
  const add = (field: string, message: string) => issues.push({ field, message });
  if (options.needsAgent && !draft.agent) add("agent", "Choose an agent.");
  if (draft.name.length > NAME_MAX) add("name", "Keep the name to 200 characters or fewer.");
  if (draft.timeZone.length > TIME_ZONE_MAX) add("timeZone", "Keep the time zone to 253 characters or fewer.");
  if (draft.frequency === "hourly" && !isInteger(draft.minute, 0, 59)) add("minute", "Enter a minute from 0 to 59.");
  if (draft.frequency === "weekly" && draft.days.length === 0) add("days", "Choose at least one day.");
  if (draft.frequency === "monthly" && !isInteger(draft.monthDay, 1, 31)) add("monthDay", "Enter a day from 1 to 31.");
  if (["daily", "weekly", "monthly"].includes(draft.frequency) && !draft.time) add("time", "Choose a time.");
  if (draft.frequency === "custom") {
    if (!draft.cron.trim()) add("cron", "Enter a cron expression.");
    else if (draft.cron.length > CRON_MAX) add("cron", "Keep the expression to 256 characters or fewer.");
  }
  if (!draft.prompt.trim()) add("prompt", "Enter a prompt.");
  else if (new TextEncoder().encode(draft.prompt).length > PROMPT_MAX_BYTES) {
    add("prompt", "Prompt must be at most 32 KiB.");
  }
  const timeout = draft.timeoutSeconds;
  if (timeout === null || !(timeout > 0) || timeout > TIMEOUT_MAX_SECONDS) {
    add("timeoutSeconds", "Enter a timeout above 0 seconds.");
  }
  return issues;
}
