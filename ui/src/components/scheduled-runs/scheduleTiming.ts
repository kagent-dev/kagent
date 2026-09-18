export const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
export const minuteIntervals = [1, 5, 10, 15, 20, 30];

// These are editor fields; arbitrary cron expressions stay in custom mode.
export interface ScheduleTiming {
  frequency: "minutes" | "hourly" | "daily" | "weekly" | "monthly" | "custom";
  interval: number;
  minute: number;
  time: string;
  days: number[];
  monthDay: number;
  cron: string;
}

export function parseSchedule(cron: string): ScheduleTiming {
  const timing: ScheduleTiming = {
    frequency: "custom", interval: 15, minute: 0, time: "09:00", days: [1], monthDay: 1, cron,
  };
  const fields = cron.trim().split(/\s+/);
  if (fields.length !== 5) return timing;
  const [minute, hour, day, month, weekday] = fields;
  if (month !== "*") return timing;
  if (hour === "*" && day === "*" && weekday === "*") {
    const interval = minute === "*" ? 1 : /^\*\/\d+$/.test(minute) ? Number(minute.slice(2)) : 0;
    if (minuteIntervals.includes(interval)) return { ...timing, frequency: "minutes", interval };
    if (/^\d+$/.test(minute) && Number(minute) < 60) {
      return { ...timing, frequency: "hourly", minute: Number(minute) };
    }
  }
  if (!/^\d+$/.test(minute) || Number(minute) > 59 || !/^\d+$/.test(hour) || Number(hour) > 23) return timing;
  timing.time = `${String(Number(hour)).padStart(2, "0")}:${String(Number(minute)).padStart(2, "0")}`;
  if (day === "*" && weekday === "*") return { ...timing, frequency: "daily" };
  if (day === "*" && (/^[0-6](,[0-6])*$/.test(weekday) || weekday === "1-5")) {
    return { ...timing, frequency: "weekly", days: weekday === "1-5" ? [1, 2, 3, 4, 5] : weekday.split(",").map(Number) };
  }
  if (/^\d+$/.test(day) && Number(day) >= 1 && Number(day) <= 31 && weekday === "*") {
    return { ...timing, frequency: "monthly", monthDay: Number(day) };
  }
  return timing;
}

export function scheduleCron(timing: ScheduleTiming): string {
  const [hour, minute] = timing.time.split(":").map(Number);
  switch (timing.frequency) {
    case "minutes": return `${timing.interval === 1 ? "*" : `*/${timing.interval}`} * * * *`;
    case "hourly": return `${timing.minute} * * * *`;
    case "daily": return `${minute} ${hour} * * *`;
    case "weekly": return `${minute} ${hour} * * ${timing.days.join(",") === "1,2,3,4,5" ? "1-5" : timing.days.join(",")}`;
    case "monthly": return `${minute} ${hour} ${timing.monthDay} * *`;
    case "custom": return timing.cron.trim();
  }
}

export function scheduleDescription(cron: string): string {
  const timing = parseSchedule(cron);
  switch (timing.frequency) {
    case "minutes": return timing.interval === 1 ? "Every minute" : `Every ${timing.interval} minutes`;
    case "hourly": return `Every hour at :${String(timing.minute).padStart(2, "0")}`;
    case "daily": return `Every day at ${timing.time}`;
    case "weekly": return `${timing.days.join(",") === "1,2,3,4,5" ? "Weekdays" : `Weekly on ${timing.days.map((day) => weekdays[day]).join(", ")}`} at ${timing.time}`;
    case "monthly": return `Monthly on day ${timing.monthDay} at ${timing.time}`;
    case "custom": return `Custom: ${cron}`;
  }
}
