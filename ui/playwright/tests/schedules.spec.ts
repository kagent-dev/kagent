import { test, expect } from "../fixtures/test";

const scheduleId = "c686bd1d-9124-4e96-8df7-000000000001";

test("schedules: edit, pause, run manually, inspect history and delete", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}?mock=ok`);
  await expect(page.getByRole("heading", { name: "Daily cluster report" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Result", exact: true })).toBeVisible();
  await expect(page.getByText("Execution deadline exceeded", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open conversation" }).first())
    .toHaveAttribute("href", "/agents/6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44/chat");
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open conversation" })).toHaveCount(1);

  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const editor = page.getByRole("dialog");
  await editor.getByLabel("Schedule Name", { exact: true }).fill("Morning report");
  await editor.getByLabel("Time zone", { exact: true }).fill("America/New_York");
  await editor.getByLabel("Prompt", { exact: true }).fill("List unhealthy workloads.");
  await editor.getByRole("button", { name: "Save changes" }).click();
  await expect(editor).toBeHidden();
  await expect(page.getByRole("heading", { name: "Morning report" })).toBeVisible();
  await expect(page.getByText("America/New_York", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeEnabled();

  // Pausing suppresses cron firings; an explicit manual invocation is still allowed.
  await page.getByRole("button", { name: "Run", exact: true }).click();
  await expect(page.getByText("Execution queued.", { exact: false })).toBeVisible();
  await expect(page.getByText("Page 1", { exact: true })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "Manual" })).toContainText("Pending");

  await page.getByRole("button", { name: "Delete schedule Morning report", exact: true }).click();
  const confirmation = page.getByRole("dialog", { name: "Delete schedule Morning report?", exact: true });
  await expect(confirmation).toContainText("Stops future executions.");
  await confirmation.getByRole("button", { name: "Keep", exact: true }).click();
  await expect(confirmation).toBeHidden();
  await expect(page.getByRole("button", { name: "Run", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Delete schedule Morning report", exact: true }).click();
  await confirmation.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("This schedule was deleted. Its execution history is retained.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Run", exact: true })).toBeDisabled();
  await expect(page.getByRole("row").filter({ hasText: "Manual" })).toContainText("Pending");
});

test("schedules: create using an existing agent", async ({ page }) => {
  await page.goto("/schedules?mock=ok");
  await page.getByRole("button", { name: "New Schedule", exact: true }).click();
  const editor = page.getByRole("dialog");
  // The first click is in the footer, which moves during the opening animation.
  await expect.poll(() => editor.evaluate((element) =>
    element.getAnimations({ subtree: true }).every((animation) => animation.playState !== "running"),
  )).toBe(true);
  await editor.getByRole("button", { name: "Create schedule", exact: true }).click();
  await expect(editor.getByText("Choose an agent.", { exact: true })).toBeVisible();
  await editor.getByLabel("Agent", { exact: true }).click();
  await page.getByTitle("kagent/k8s-agent-7f3a91c on k8s-agent", { exact: true }).click();
  await editor.getByLabel("Schedule Name", { exact: true }).fill("Weekly report");
  await expect(editor.getByLabel("Cron expression", { exact: true })).toHaveCount(0);
  await editor.getByLabel("Repeat", { exact: true }).click();
  await page.getByTitle("Weekly", { exact: true }).click();
  await editor.getByLabel("At time", { exact: true }).fill("08:00");
  await editor.getByLabel("Wednesday", { exact: true }).check();
  await expect(editor.getByRole("status")).toHaveText("Weekly on Monday, Wednesday at 08:00 (UTC)");
  await editor.getByLabel("Prompt", { exact: true }).fill("Summarize this week.");
  await editor.getByLabel("Execution timeout (seconds)", { exact: true }).fill("120");
  await expect(editor.getByLabel("Enable Schedule", { exact: true })).toBeChecked();
  await expect(editor.getByText("This schedule will run automatically after it is created.")).toBeVisible();
  await editor.getByLabel("Enable Schedule", { exact: true }).uncheck();
  await expect(editor.getByText("This schedule will not run automatically after it is created.")).toBeVisible();
  await editor.getByRole("button", { name: "Create schedule", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Weekly report", exact: true })).toBeVisible();
  await expect(page.getByText("Weekly on Monday, Wednesday at 08:00", { exact: true })).toBeVisible();
  await expect(page.getByText("120 seconds", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(editor.getByLabel("Enable Schedule", { exact: true })).not.toBeChecked();
  await expect(editor.getByText("This schedule will not run automatically after it is saved.")).toBeVisible();
  await editor.getByLabel("Enable Schedule", { exact: true }).check();
  await expect(editor.getByText("This schedule will run automatically after it is saved.")).toBeVisible();
  await expect(editor.getByLabel("At time", { exact: true })).toHaveValue("08:00");
  await expect(editor.getByLabel("Monday", { exact: true })).toBeChecked();
  await expect(editor.getByLabel("Wednesday", { exact: true })).toBeChecked();
  await editor.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(editor).toBeHidden();
  await expect(page.getByRole("button", { name: "Pause", exact: true })).toBeEnabled();
  await expect(page.getByText("No executions yet", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Back to schedules" }).click();
  await expect(page.getByRole("link", { name: "Weekly report", exact: true })).toBeVisible();
});

test("schedules: read failures stay distinct from an empty list", async ({ page }) => {
  await page.goto("/schedules?mock=error");
  await expect(page.getByText("Could not load schedules", { exact: true })).toBeVisible();
  await expect(page.getByText("No schedules yet", { exact: true })).toHaveCount(0);
  await page.goto("/schedules?mock=empty");
  await expect(page.getByText("No schedules yet", { exact: true })).toBeVisible();
});


test("schedules: preserve advanced expressions when editing other fields", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}?mock=ok`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const editor = page.getByRole("dialog");
  await expect.poll(() => editor.evaluate((element) =>
    element.getAnimations({ subtree: true }).every((animation) => animation.playState !== "running"),
  )).toBe(true);
  await editor.getByLabel("Repeat", { exact: true }).click();
  await page.getByTitle("Custom (advanced)", { exact: true }).click();
  await expect(editor.getByLabel("Cron expression", { exact: true })).toHaveValue("0 9 * * *");
  await editor.getByLabel("Cron expression", { exact: true }).fill("0 9-17 * * 1-5");
  await editor.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(editor).toBeHidden();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(editor.getByLabel("Cron expression", { exact: true })).toHaveValue("0 9-17 * * 1-5");
  await editor.getByLabel("Prompt", { exact: true }).fill("Preserve business hours.");
  await editor.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(editor).toBeHidden();
  await expect(page.getByText("Custom: 0 9-17 * * 1-5", { exact: true })).toBeVisible();
});
