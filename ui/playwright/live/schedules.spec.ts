import { test, expect } from "../fixtures/test";
import { loadLive, throwawayName } from "./helpers/live";

// Requires the lifecycle fixture's ready kagent/smoke agent. Keep it paused:
// runtime execution is covered by the Go scheduling E2Es with a controlled model.
test("live: schedule configuration persists through the browser and controller", async ({ page }) => {
  const name = throwawayName("schedule");
  let detailURL: string | undefined;
  try {
    await loadLive(page, "/schedules");
    await page.getByRole("button", { name: "New schedule", exact: true }).click();
    const editor = page.getByRole("dialog");
    await editor.getByLabel("Agent", { exact: true }).click();
    await page.getByTitle("kagent/smoke on kagent", { exact: true }).click();
    await editor.getByLabel("Name", { exact: true }).fill(name);
    await editor.getByLabel("Prompt", { exact: true }).fill("Report cluster health.");
    await editor.getByLabel("Repeat", { exact: true }).click();
    await page.getByTitle("Weekly", { exact: true }).click();
    for (const day of ["Tuesday", "Wednesday", "Thursday", "Friday"]) {
      await editor.getByLabel(day, { exact: true }).check();
    }
    await editor.getByLabel("At time", { exact: true }).fill("09:00");
    await editor.getByLabel("Time zone", { exact: true }).fill("America/New_York");
    await editor.getByLabel("Execution timeout (seconds)", { exact: true }).fill("90.001");
    await editor.getByLabel("Paused", { exact: true }).check();
    await editor.getByRole("button", { name: "Create schedule", exact: true }).click();
    await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
    detailURL = page.url();
    await page.reload();
    await expect(page.getByRole("button", { name: "Resume schedule", exact: true })).toBeEnabled();
    await expect(page.getByText("Weekdays at 09:00", { exact: true })).toBeVisible();
    await expect(page.getByText("90.001 seconds", { exact: true })).toBeVisible();
    await expect(page.getByText("America/New_York", { exact: true })).toBeVisible();
    await expect(page.getByText("No executions yet", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Edit schedule", exact: true }).click();
    await editor.getByLabel("Time zone", { exact: true }).fill("");
    await expect(editor.getByRole("status")).toHaveText("Weekdays at 09:00 (UTC)");
    await editor.getByLabel("Time zone", { exact: true }).fill("America/New_York");
    await editor.getByLabel("Prompt", { exact: true }).fill("Report unhealthy workloads only.");
    await editor.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(editor).toBeHidden();
    await page.reload();
    await expect(page.getByText("Report unhealthy workloads only.", { exact: true })).toBeVisible();
    await expect(page.getByText("90.001 seconds", { exact: true })).toBeVisible();
  } finally {
    if (detailURL) {
      await page.goto(detailURL);
      await page.getByRole("button", { name: `Delete schedule ${name}`, exact: true }).click();
      await page.getByRole("button", { name: "Delete", exact: true }).click();
      await expect(page.getByText("This schedule was deleted. Its execution history is retained.")).toBeVisible();
      await expect(page.getByRole("button", { name: "Run now", exact: true })).toBeDisabled();
      await page.getByRole("link", { name: "Back to schedules" }).click();
      await expect(page.getByRole("link", { name, exact: true })).toHaveCount(0);
    }
  }
});
