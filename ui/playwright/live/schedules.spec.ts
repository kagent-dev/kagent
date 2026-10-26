import { test, expect } from "../fixtures/test";
import { loadLive, throwawayName } from "./helpers/live";
import { liveApi } from "./helpers/api";
import { tick } from "../helpers/controls";
import { selectOption } from "../helpers/resource";

// Creates an Agent using the standard kagent Harness and default ModelConfig.
// Keep the schedule paused; runtime execution is covered by the Go E2Es.
test("live: schedule configuration persists through the browser and controller", async ({ page, baseURL }) => {
  const name = throwawayName("schedule");
  const api = liveApi(baseURL!);
  const agentName = throwawayName("schedule-agent");
  let detailURL: string | undefined;
  try {
    await api.createAgent("kagent", agentName, { harnessRef: { name: "kagent" }, template: { inline: { modelConfig: { name: "default-model-config" }, systemPrompt: "Summarize cluster health." } } });
    await loadLive(page, "/schedules");
    await page.getByRole("button", { name: "New Schedule", exact: true }).click();
    await selectOption(page, "schedule-namespace", "kagent");
    await expect(page.locator(".ant-select-dropdown:visible")).toHaveCount(0);
    await selectOption(page, "schedule-agent", agentName);
    await page.getByTestId("schedule-name").fill(name);
    await page.getByLabel("Prompt", { exact: true }).fill("Report cluster health.");
    await selectOption(page, "schedule-frequency", "Weekly");
    for (const day of ["Tuesday", "Wednesday", "Thursday", "Friday"]) {
      // `tick` rather than `check()`, which double-toggles when the card re-renders
      // under it.
      await tick(page.getByLabel(day, { exact: true }));
    }
    await page.getByLabel("At time", { exact: true }).fill("09:00");
    await page.getByLabel("Time zone", { exact: true }).fill("America/New_York");
    await page.getByLabel("Execution timeout", { exact: true }).fill("90.001s");
    await page.getByLabel("Enable Schedule", { exact: true }).uncheck();
    await page.getByRole("button", { name: "Create schedule", exact: true }).click();
    await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
    detailURL = page.url();
    await page.reload();
    await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeEnabled();
    await expect(page.getByText("Weekdays at 09:00", { exact: true })).toBeVisible();
    // Kubernetes normalizes the duration when it persists the CRD.
    await expect(page.getByText("1m30.001s", { exact: true })).toBeVisible();
    await expect(page.getByText("America/New_York", { exact: true })).toBeVisible();
    await expect(page.getByText("No executions yet", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(page.getByLabel("Enable Schedule", { exact: true })).not.toBeChecked();
    await expect(page.getByTestId("schedule-agent")).toContainText(agentName);
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
    await expect(page.getByText("1m30.001s", { exact: true })).toBeVisible();
  } finally {
    try {
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
    } finally { await api.removeAgent("kagent", agentName); }
  }
});
