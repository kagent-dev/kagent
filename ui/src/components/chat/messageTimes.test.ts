import { describe, expect, it } from "vitest";
import { dayLabel, parseMessageTime, planMessageTimes, timeLabel } from "./messageTimes";

const NOW = new Date(2026, 10, 20, 15, 0);
const at = (day: number, hour: number, minute: number, month = 10, year = 2026) =>
  new Date(year, month, day, hour, minute).toISOString();
// ICU separates "PM" with a narrow no-break space; compare on plain spaces.
const plain = (text: string) => text.replace(/\s/g, " ");

describe("parseMessageTime", () => {
  it("returns undefined for a missing or unreadable timestamp", () => {
    expect(parseMessageTime("")).toBeUndefined();
    expect(parseMessageTime(undefined)).toBeUndefined();
    expect(parseMessageTime("not a date")).toBeUndefined();
  });
});

describe("dayLabel", () => {
  it.each([
    ["today", at(20, 9, 0), "Today"],
    ["yesterday, across midnight", at(19, 23, 59), "Yesterday"],
    ["earlier this year", at(17, 10, 0), "Tue, Nov 17"],
    ["an earlier year", at(3, 10, 0, 10, 2025), "Nov 3, 2025"],
  ])("labels %s", (_, iso, expected) => {
    expect(dayLabel(new Date(iso), NOW, "en-US")).toBe(expected);
  });

  it("finds yesterday across a month boundary", () => {
    expect(dayLabel(new Date(2026, 10, 30, 22), new Date(2026, 11, 1, 8), "en-US")).toBe(
      "Yesterday",
    );
  });
});

describe("timeLabel", () => {
  it("formats the time of day in the given locale", () => {
    expect(plain(timeLabel(new Date(2026, 10, 20, 14, 41), "en-US"))).toBe("2:41 PM");
  });
});

describe("planMessageTimes", () => {
  it("opens each day with a label and each sender's run with a time", () => {
    const plan = planMessageTimes(
      [
        { id: "a", role: "user", createdAt: at(19, 10, 0) },
        { id: "b", role: "agent", createdAt: at(19, 10, 1) },
        { id: "c", role: "agent", createdAt: at(19, 10, 4) },
        { id: "d", role: "agent", createdAt: at(19, 10, 30) },
        { id: "e", role: "user", createdAt: at(20, 9, 0) },
      ],
      NOW,
      "en-US",
    );

    expect([...plan.values()].map((time) => time.dayLabel)).toEqual([
      "Yesterday",
      undefined,
      undefined,
      undefined,
      "Today",
    ]);
    expect([...plan.values()].map((time) => time.showTime)).toEqual([
      true,
      true,
      false,
      true,
      true,
    ]);
  });

  it("shows nothing for a message without a timestamp and keeps the run around it", () => {
    const plan = planMessageTimes(
      [
        { id: "a", role: "agent", createdAt: at(20, 9, 0) },
        { id: "b", role: "agent", createdAt: "" },
        { id: "c", role: "agent", createdAt: at(20, 9, 2) },
      ],
      NOW,
      "en-US",
    );

    expect(plan.get("b")).toEqual({ showTime: false });
    expect(plan.get("c")).toMatchObject({ dayLabel: undefined, showTime: false });
  });
});
