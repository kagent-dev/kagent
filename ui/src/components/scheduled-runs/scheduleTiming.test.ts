import { describe, expect, it } from "vitest";
import { parseSchedule, scheduleCron, scheduleDescription } from "./scheduleTiming";

describe("schedule controls", () => {
  it.each([
    ["* * * * *", "minutes", "Every minute"],
    ["*/15 * * * *", "minutes", "Every 15 minutes"],
    ["5 * * * *", "hourly", "Every hour at :05"],
    ["0 0 * * *", "daily", "Every day at 00:00"],
    ["45 23 * * *", "daily", "Every day at 23:45"],
    ["0 9 * * 1-5", "weekly", "Weekdays at 09:00"],
    ["30 8 * * 1,3,5", "weekly", "Weekly on Monday, Wednesday, Friday at 08:30"],
    ["0 12 * * 0", "weekly", "Weekly on Sunday at 12:00"],
    ["0 9 31 * *", "monthly", "Monthly on day 31 at 09:00"],
  ])("round-trips %s through its controls", (cron, frequency, description) => {
    const timing = parseSchedule(cron);
    expect(timing.frequency).toBe(frequency);
    expect(scheduleCron(timing)).toBe(cron);
    expect(scheduleDescription(cron)).toBe(description);
  });

  it.each([
    "*/7 * * * *", // Not a constant interval across the hour boundary.
    "0 9 1 * 1", // Cron combines day-of-month and day-of-week with OR.
    "0 9 * 1 *",
    "0 9-17 * * 1-5",
    "0 9 * * MON-FRI",
    "0 9 * * 7",
    "0 24 * * *",
    "60 9 * * *",
    "0 9 32 * *",
    "0 9 * * * *",
  ])("preserves unsupported expressions in advanced mode: %s", (cron) => {
    expect(parseSchedule(cron).frequency).toBe("custom");
    expect(scheduleCron(parseSchedule(cron))).toBe(cron);
  });
});
