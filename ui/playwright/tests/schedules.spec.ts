import { test, expect } from "../fixtures/test";

const scheduleId = "c686bd1d-9124-4e96-8df7-000000000001";

test("schedules: edit, pause, run manually, inspect history and delete", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}?mock=ok`);
  await expect(page.getByRole("heading", { name: "Daily cluster report" })).toBeVisible();
  // The header carries the cadence and the clock as pills; the list below carries
  // the rest.
  await expect(page.getByTestId("schedule-meta")).toContainText("Every day at 09:00");
  await expect(page.getByTestId("schedule-meta")).toContainText("UTC");
  // A filled Run: it is the action this page exists for.
  await expect(page.getByRole("button", { name: "Run", exact: true })).toHaveClass(/ant-btn-primary/);
  await expect(page.getByRole("columnheader", { name: "Failure reason", exact: true })).toBeVisible();
  await expect(page.getByText("Execution deadline exceeded", { exact: true })).toBeVisible();
  const conversation = page.getByRole("link", { name: "Open conversation" }).first();
  await expect(conversation).toHaveAttribute("href", "/agents/6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44/chat");
  await expect(conversation.locator("svg")).toHaveCount(1);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open conversation" })).toHaveCount(1);

  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(page).toHaveURL(`/schedules/${scheduleId}/edit`);
  await page.getByLabel("Schedule Name", { exact: true }).fill("Morning report");
  await page.getByLabel("Time zone", { exact: true }).fill("America/New_York");
  await page.getByLabel("Prompt", { exact: true }).fill("List unhealthy workloads.");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page).toHaveURL(`/schedules/${scheduleId}`);
  await expect(page.getByRole("heading", { name: "Morning report" })).toBeVisible();
  await expect(page.getByTestId("schedule-meta")).toContainText("America/New_York");
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeEnabled();
  await expect(page.getByTestId("schedule-meta")).toContainText("Paused");

  // Pausing suppresses cron firings; an explicit manual invocation is still allowed.
  await page.getByRole("button", { name: "Run", exact: true }).click();
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
  // Deleting leaves for the list, which is where the reader can act next: this page is
  // now about a schedule that is gone, and the list does not carry it.
  await expect(page).toHaveURL(/\/schedules(\?.*)?$/);
  await expect(page.getByRole("link", { name: "Morning report", exact: true })).toHaveCount(0);

});

/* A link held from before the delete still opens, and says what it is looking at. */
test("schedules: a deleted schedule opens by address and says so", async ({ page }) => {
  await page.goto("/schedules/c686bd1d-9124-4e96-8df7-000000000004?mock=ok");
  await expect(page.getByRole("heading", { name: "Retired sweep" })).toBeVisible();
  await expect(page.getByText("This schedule was deleted. Its execution history is retained.")).toBeVisible();
  for (const name of ["Run", "Pause", "Edit"]) {
    await expect(page.getByRole("button", { name, exact: true })).toBeDisabled();
  }
  await expect(page.getByTestId("schedule-meta")).toContainText("Deleted");
  // And it is not offered in the list it was removed from.
  await page.goto("/schedules?mock=ok");
  await expect(page.getByRole("link", { name: "Retired sweep", exact: true })).toHaveCount(0);
});

test("schedules: create using an existing agent", async ({ page }) => {
  await page.goto("/schedules?mock=ok");
  await page.getByRole("button", { name: "New Schedule", exact: true }).click();
  await expect(page).toHaveURL("/schedules/new");
  await expect(page.getByRole("heading", { name: "New schedule", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Create schedule", exact: true }).click();
  await expect(page.getByText("Choose an agent.", { exact: true })).toBeVisible();
  await page.getByLabel("Agent", { exact: true }).click();
  await page.getByTitle("kagent/k8s-agent-7f3a91c on k8s-agent", { exact: true }).click();
  await page.getByLabel("Schedule Name", { exact: true }).fill("Weekly report");
  await expect(page.getByLabel("Cron expression", { exact: true })).toHaveCount(0);
  await page.getByLabel("Repeat", { exact: true }).click();
  await page.getByTitle("Weekly", { exact: true }).click();
  await page.getByLabel("At time", { exact: true }).fill("08:00");
  await page.getByLabel("Wednesday", { exact: true }).check();
  await expect(page.getByRole("status")).toHaveText("Weekly on Monday, Wednesday at 08:00 (UTC)");
  await page.getByLabel("Prompt", { exact: true }).fill("Summarize this week.");
  await page.getByLabel("Execution timeout (seconds)", { exact: true }).fill("120");
  await expect(page.getByLabel("Enable Schedule", { exact: true })).toBeChecked();
  await expect(page.getByText("This schedule will run automatically after it is created.")).toBeVisible();
  await page.getByLabel("Enable Schedule", { exact: true }).uncheck();
  await expect(page.getByText("This schedule will not run automatically after it is created.")).toBeVisible();
  await page.getByRole("button", { name: "Create schedule", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Weekly report", exact: true })).toBeVisible();
  await expect(page.getByTestId("schedule-meta")).toContainText("Weekly on Monday, Wednesday at 08:00");
  await expect(page.getByText("120 seconds", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Edit Weekly report", exact: true })).toBeVisible();
  await expect(page.getByLabel("Enable Schedule", { exact: true })).not.toBeChecked();
  await expect(page.getByText("This schedule will not run automatically after it is saved.")).toBeVisible();
  await page.getByLabel("Enable Schedule", { exact: true }).check();
  await expect(page.getByText("This schedule will run automatically after it is saved.")).toBeVisible();
  await expect(page.getByLabel("At time", { exact: true })).toHaveValue("08:00");
  await expect(page.getByLabel("Monday", { exact: true })).toBeChecked();
  await expect(page.getByLabel("Wednesday", { exact: true })).toBeChecked();
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(page.getByRole("button", { name: "Pause", exact: true })).toBeEnabled();
  await expect(page.getByText("No executions yet", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Back", exact: true }).click();
  await expect(page.getByRole("link", { name: "Weekly report", exact: true })).toBeVisible();
});

/*
 * One schedule made, read back, changed and removed — all from the list, which is the
 * only surface that can say whether any of it happened. A closed form and a redirect
 * only prove the app believes it worked.
 */
test("schedules: create, read, update and delete from the list", async ({ page }) => {
  await page.goto("/schedules?mock=ok");
  const rows = page.getByRole("row");
  const rowNamed = (name: string) => rows.filter({ hasText: name });
  await expect(page.getByRole("link", { name: "Daily cluster report", exact: true })).toBeVisible();
  const before = await rows.count();

  await page.getByTestId("schedules-new").click();
  await page.getByLabel("Agent", { exact: true }).click();
  await page.getByTitle("kagent/k8s-agent-7f3a91c on k8s-agent", { exact: true }).click();
  await page.getByLabel("Schedule Name", { exact: true }).fill("Probe alpha");
  await page.getByLabel("Prompt", { exact: true }).fill("Check the probe.");
  await page.getByTestId("schedule-submit").click();
  await expect(page.getByRole("heading", { name: "Probe alpha", exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Back", exact: true }).click();
  await expect(rowNamed("Probe alpha")).toHaveCount(1);
  await expect(rowNamed("Probe alpha")).toContainText("Every day at 09:00");
  await expect(rows).toHaveCount(before + 1);

  await page.getByTestId("edit-Probe alpha").click();
  await expect(page.getByRole("heading", { name: "Edit Probe alpha", exact: true })).toBeVisible();
  await page.getByLabel("Schedule Name", { exact: true }).fill("Probe beta");
  await page.getByLabel("Time zone", { exact: true }).fill("Europe/Berlin");
  // The zone list is an autocomplete; dismiss it so it is not over the form.
  await page.keyboard.press("Escape");
  await page.getByTestId("schedule-submit").click();
  await expect(page.getByRole("heading", { name: "Probe beta", exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Back", exact: true }).click();
  await expect(rowNamed("Probe beta")).toHaveCount(1);
  await expect(rowNamed("Probe beta")).toContainText("Europe/Berlin");
  // Renamed, not duplicated.
  await expect(rowNamed("Probe alpha")).toHaveCount(0);
  await expect(rows).toHaveCount(before + 1);

  await page.getByRole("button", { name: "Delete schedule Probe beta", exact: true }).click();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(rowNamed("Probe beta")).toHaveCount(0);
  // One row went, not the table.
  await expect(rows).toHaveCount(before);
  await expect(page.getByRole("link", { name: "Daily cluster report", exact: true })).toBeVisible();
});

test("schedules: read failures stay distinct from an empty list", async ({ page }) => {
  await page.goto("/schedules?mock=error");
  await expect(page.getByText("Could not load schedules", { exact: true })).toBeVisible();
  await expect(page.getByText("No schedules yet", { exact: true })).toHaveCount(0);
  await page.goto("/schedules?mock=empty");
  await expect(page.getByText("No schedules yet", { exact: true })).toBeVisible();
});

test("schedules: a list that fits on one page shows no pagination", async ({ page }) => {
  await page.goto("/schedules?mock=ok");
  await expect(page.getByRole("link", { name: "Daily cluster report", exact: true })).toBeVisible();
  await expect(page.getByTestId("schedules-pages")).toHaveCount(0);
});

test("schedules: /schedules/new is the create page, not a schedule called \"new\"", async ({ page }) => {
  await page.goto("/schedules/new?mock=ok");
  await expect(page.getByRole("heading", { name: "New schedule", exact: true })).toBeVisible();
  await expect(page.getByLabel("Agent", { exact: true })).toBeVisible();
  await expect(page.getByText("Execution history", { exact: true })).toHaveCount(0);
});

test("schedules: preserve advanced expressions when editing other fields", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}/edit?mock=ok`);
  await page.getByLabel("Repeat", { exact: true }).click();
  await page.getByTitle("Custom (advanced)", { exact: true }).click();
  await expect(page.getByLabel("Cron expression", { exact: true })).toHaveValue("0 9 * * *");
  await page.getByLabel("Cron expression", { exact: true }).fill("0 9-17 * * 1-5");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(page).toHaveURL(`/schedules/${scheduleId}`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(page.getByLabel("Cron expression", { exact: true })).toHaveValue("0 9-17 * * 1-5");
  await page.getByLabel("Prompt", { exact: true }).fill("Preserve business hours.");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(page).toHaveURL(`/schedules/${scheduleId}`);
  await expect(page.getByTestId("schedule-meta")).toContainText("Custom: 0 9-17 * * 1-5");
});

/*
 * The execution history row behaves like every other expandable row in the app: the
 * whole row opens it, except where the row holds something separately clickable. The
 * conversation link is that exception, and it is the reason the table cannot use
 * antd's `expandRowByClick`.
 */
test("schedules: an execution row expands on a click, but its conversation link navigates", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}?mock=ok`);
  const row = page.getByRole("row").filter({ hasText: "Execution deadline exceeded" });
  await expect(row).toHaveClass(/clickable-table-row/);

  await test.step("a click on the row's own text expands it", async () => {
    await row.getByRole("cell").filter({ hasText: "Timed out" }).click();
    await expect(page.getByText("Original task", { exact: true })).toBeVisible();
    await expect(page.getByText("mock-scheduled-task-1", { exact: true })).toBeVisible();
  });

  await test.step("the expand icon still collapses it, so a keyboard reaches the panel", async () => {
    await row.locator(".ant-table-row-expand-icon").click();
    await expect(page.getByText("Original task", { exact: true })).toBeHidden();
  });

  await test.step("the conversation link navigates instead of expanding", async () => {
    await row.getByRole("link", { name: "Open conversation" }).click();
    await expect(page).toHaveURL("/agents/6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44/chat");
    // The guard silently ceasing to match is the failure this pins: the click would
    // then unfold the row on the way out, and only this assertion would notice.
    await expect(page.getByText("Original task", { exact: true })).toHaveCount(0);
  });
});

/*
 * Expand all works on what is on screen, which is why the search sits beside it: the two
 * compose, and "all" after a search means the matches rather than the page.
 */
test("schedules: execution history expands as a whole and searches the loaded page", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}?mock=ok`);
  const expandAll = page.getByTestId("history-expand-all");
  const panels = page.locator(".ant-table-expanded-row:visible");
  const rows = page.locator("tbody tr.ant-table-row");
  await expect(rows).toHaveCount(25);

  await test.step("expand all opens every row, and the label turns around", async () => {
    await expect(expandAll).toHaveText("Expand all");
    await expandAll.click();
    await expect(panels).toHaveCount(25);
    await expect(expandAll).toHaveText("Collapse all");
  });

  await test.step("collapse all returns it to where it started", async () => {
    await expandAll.click();
    await expect(panels).toHaveCount(0);
    await expect(expandAll).toHaveText("Expand all");
  });

  await test.step("searching narrows to the matching rows", async () => {
    // The state as it is rendered, not the enum: one row of the 25 timed out.
    await page.getByTestId("history-search").fill("Timed out");
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toContainText("Execution deadline exceeded");
  });

  await test.step("a row matching only inside its panel comes back opened", async () => {
    // `mock-scheduled-task-3` is the original task ID, which is only in the panel.
    await page.getByTestId("history-search").fill("mock-scheduled-task-3");
    await expect(rows).toHaveCount(1);
    await expect(panels).toHaveCount(1);
    await expect(page.getByText("mock-scheduled-task-3", { exact: true })).toBeVisible();
  });

  await test.step("a revealed row is still the reader's to close", async () => {
    // It was not: `revealed` used to be unioned into the expanded keys on every render,
    // so the click removed the key and the reveal put it straight back.
    await expect(expandAll).toHaveText("Collapse all");
    await rows.first().click();
    await expect(panels).toHaveCount(0);
    await rows.first().click();
    await expect(panels).toHaveCount(1);
    // A different search seeds again rather than remembering the last collapse.
    await page.getByTestId("history-search").fill("mock-scheduled-task-4");
    await expect(panels).toHaveCount(1);
  });

  await test.step("a search matching nothing is not an empty history", async () => {
    await page.getByTestId("history-search").fill("nothing matches this");
    await expect(page.getByText("Nothing on this page of the history matches that search.")).toBeVisible();
    await expect(page.getByText("No executions yet", { exact: true })).toHaveCount(0);
  });
});

/*
 * The row is a mouse affordance over the link that was already there. Both paths are
 * asserted, because losing the link would take keyboard users' only way in.
 */
test("schedules: a list row opens its schedule, and its buttons still do their own job", async ({ page }) => {
  await page.goto("/schedules?mock=ok");
  const row = page.getByRole("row").filter({ hasText: "Daily cluster report" });
  await expect(row).toHaveClass(/clickable-table-row/);

  await test.step("a click on the row's own text opens the schedule", async () => {
    await row.getByRole("cell").filter({ hasText: "k8s-agent-7f3a91c" }).click();
    await expect(page).toHaveURL(`/schedules/${scheduleId}`);
  });

  await test.step("the edit button edits rather than opening the detail page", async () => {
    await page.goto("/schedules?mock=ok");
    await page.getByTestId("edit-Daily cluster report").click();
    await expect(page).toHaveURL(`/schedules/${scheduleId}/edit`);
  });

  await test.step("the delete button asks, and navigates nowhere", async () => {
    await page.goto("/schedules?mock=ok");
    await page.getByRole("button", { name: "Delete schedule Daily cluster report", exact: true }).click();
    await expect(page.getByRole("button", { name: "Keep", exact: true })).toBeVisible();
    await expect(page).toHaveURL(/\/schedules(\?.*)?$/);
  });
});

/*
 * Delete lives at the foot of the page rather than beside Run. Asserted by position,
 * not only by presence: the point of the move is that the irreversible control is not
 * one a reader reaches while aiming at Run.
 */
test("schedules: delete sits in its own section at the foot, not in the header", async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}?mock=ok`);
  const danger = page.getByTestId("schedule-danger");
  // Level 3, the same as "Execution history": the two are siblings, not one inside the other.
  await expect(danger.getByRole("heading", { level: 3, name: "Danger zone" })).toBeVisible();
  await expect(danger).toContainText("Deleting this schedule stops future executions.");
  await expect(danger.getByRole("button", { name: "Delete schedule Daily cluster report", exact: true })).toBeVisible();

  // The header keeps the five reversible controls and none of the destructive one.
  const header = page.getByRole("button", { name: /^(Run|Pause|Resume|Edit|Refresh)$/ });
  await expect(header).toHaveCount(4);
  await expect(page.getByRole("button", { name: /^Delete/ })).toHaveCount(1);

  await expect(page.getByRole("link", { name: "Back", exact: true })).toHaveAttribute("href", "/schedules");
});
