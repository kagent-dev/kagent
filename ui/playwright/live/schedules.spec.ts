import { test, expect } from "../fixtures/test";
import { loadLive, throwawayName } from "./helpers/live";
import { tick } from "../helpers/controls";

// Requires one agent with a ready revision, and takes whichever that is. Keep the
// schedule paused: runtime execution is covered by the Go scheduling E2Es with a
// controlled model.
test("live: schedule configuration persists through the browser and controller", async ({ page }) => {
  const name = throwawayName("schedule");
  let detailURL: string | undefined;
  try {
    await loadLive(page, "/schedules");
    await page.getByRole("button", { name: "New Schedule", exact: true }).click();
    await page.getByLabel("Agent", { exact: true }).click();
    // Whichever agent the fleet offers, rather than `kagent/smoke on kagent` by name:
    // #2422 split an agent into a template and a harness, and the form appends a
    // reason to the label of any whose revision is not ready, so an exact title cannot
    // match. Disabled ones are excluded because antd renders its "no match" and loading
    // rows as options too.
    await page
      .locator(".ant-select-dropdown:not(.ant-select-dropdown-hidden)")
      .locator(".ant-select-item-option:not(.ant-select-item-option-disabled)")
      .first()
      .click();
    await page.getByLabel("Schedule Name", { exact: true }).fill(name);
    await page.getByLabel("Prompt", { exact: true }).fill("Report cluster health.");
    await page.getByLabel("Repeat", { exact: true }).click();
    await page.getByTitle("Weekly", { exact: true }).click();
    for (const day of ["Tuesday", "Wednesday", "Thursday", "Friday"]) {
      // `tick` rather than `check()`, which double-toggles when the card re-renders
      // under it.
      await tick(page.getByLabel(day, { exact: true }));
    }
    await page.getByLabel("At time", { exact: true }).fill("09:00");
    await page.getByLabel("Time zone", { exact: true }).fill("America/New_York");
    await page.getByLabel("Execution timeout (seconds)", { exact: true }).fill("90.001");
    await page.getByLabel("Enable Schedule", { exact: true }).uncheck();
    await page.getByRole("button", { name: "Create schedule", exact: true }).click();
    await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
    detailURL = page.url();
    await page.reload();
    await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeEnabled();
    await expect(page.getByText("Weekdays at 09:00", { exact: true })).toBeVisible();
    await expect(page.getByText("90.001 seconds", { exact: true })).toBeVisible();
    await expect(page.getByText("America/New_York", { exact: true })).toBeVisible();
    await expect(page.getByText("No executions yet", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(page.getByLabel("Enable Schedule", { exact: true })).not.toBeChecked();
    await expect(page.getByText("This schedule will not run automatically after it is saved.")).toBeVisible();
    await page.getByLabel("Time zone", { exact: true }).fill("");
    await expect(page.getByRole("status")).toHaveText("Weekdays at 09:00 (UTC)");
    await page.getByLabel("Time zone", { exact: true }).fill("America/New_York");
    await page.getByLabel("Prompt", { exact: true }).fill("Report unhealthy workloads only.");
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    // The form is its own page, so a save leaves it rather than closing it.
    await expect(page).toHaveURL(detailURL);
    await page.reload();
    await expect(page.getByText("Report unhealthy workloads only.", { exact: true })).toBeVisible();
    await expect(page.getByText("90.001 seconds", { exact: true })).toBeVisible();
  } finally {
    if (detailURL) {
      await page.goto(detailURL);
      await page.getByRole("button", { name: `Delete schedule ${name}`, exact: true }).click();
      const listRead = page.waitForResponse((response) =>
        response.url().includes("ScheduledRunService/ListScheduledRuns"));
      await page.getByRole("dialog").getByRole("button", { name: "Delete", exact: true }).click();
      // Deleting navigates back to the list, so the detail page's own controls are gone.
      await expect(page).toHaveURL(/\/schedules(?:\?.*)?$/);
      await listRead;
      // The response can arrive before SWR has rendered it; a loading list is also empty.
      await expect(page.locator(".ant-spin").filter({ has: page.getByTestId("schedules-table") }))
        .toHaveAttribute("aria-busy", "false");
      await expect(page.getByTestId("schedules-error")).toHaveCount(0);
      await expect(page.getByTestId(`schedule-link-${name}`)).toHaveCount(0);
    }
  }
});
