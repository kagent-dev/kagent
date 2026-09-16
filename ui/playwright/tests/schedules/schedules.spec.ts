import { test, expect } from "../../fixtures/test";
import { LIFECYCLE_TIMEOUT, optionNamed } from "../../helpers/resource";

/**
 * Schedules — reading one, running it, and the states around that.
 *
 * **The write journey is not here.** Creating a schedule, reading it back, changing it
 * and deleting it runs against both backends from `shared/schedules/schedules.spec.ts`;
 * the claim that it survives a reload — which the fixtures structurally cannot answer,
 * keeping writes in the page's own memory — is `live/schedules.spec.ts`.
 *
 * One test, because a video and a trace are recorded per *test* — see
 * `playwright/README.md`.
 *
 * ## What is distinctive about this resource, and therefore what is covered
 *
 * **A schedule is the only resource here that runs.** So the journey covers pausing one,
 * invoking it by hand while paused, and reading the execution it produced — none of which
 * any other resource has, and all of which is the reason a schedule exists.
 *
 * **Its cadence has two representations.** The form offers a repeat picker and an
 * advanced cron expression, and an expression the picker cannot represent has to survive
 * an edit to some other field. That is asserted because it is the one that silently
 * destroys a reader's work.
 *
 * **Its history outlives it.** A deleted schedule still opens by address and says what it
 * is, because the executions are retained — so the address is not a 404 and must not be
 * rendered as a live schedule either.
 *
 * ## What is still read as prose, deliberately
 *
 * Five selectors, and each is the right tool rather than a leftover. Two are fixture
 * *data* in a cell — an execution's failure reason, its task id — which is the thing
 * under test and has no id to give it. One is a form rule's message. The last two are
 * `getByLabel("Monday")` on the weekday checkboxes, which are genuinely labelled
 * controls: a label is what a reader clicks and what a screen reader announces, so
 * reaching for one is not the same as matching copy.
 *
 * ## Why it clicks through rather than navigating
 *
 * The mock backend keeps writes in the page's own memory, so a `page.goto` starts a
 * backend that has never heard of a change just made. The steps below therefore click
 * through rather than navigate wherever a write has to outlive the step that made it.
 */

/** `Daily cluster report`, the seeded schedule the read half is asserted against. */
const SEEDED = "c686bd1d-9124-4e96-8df7-000000000001";

/** A schedule deleted before the fixtures were written, kept for its history. */
const RETIRED = "c686bd1d-9124-4e96-8df7-000000000004";

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("schedules: a schedule is read, run, paused, and its failures reported", async ({
  page,
}) => {
  const rows = page.getByRole("row");
  const rowNamed = (name: string) => rows.filter({ hasText: name });

  await test.step("1. the list carries the seeded schedule, and fits on one page", async () => {
    await page.goto("/schedules?mock=ok");
    await expect(
      page.getByTestId("schedule-link-Daily cluster report"),
    ).toBeVisible();
    // No pagination over a list this size: a control that pages nothing is a control
    // that implies there is more to see.
    await expect(page.getByTestId("schedules-pages")).toHaveCount(0);
  });

  await test.step("2. a row opens its schedule, and its buttons still do their own job", async () => {
    // The row is a mouse affordance over the link that was already there. Both paths
    // matter: losing the link would take keyboard users' only way in.
    const row = rowNamed("Daily cluster report");
    await expect(row).toHaveClass(/clickable-table-row/);
    await row.getByRole("cell").filter({ hasText: "k8s-agent-7f3a91c" }).click();
    await expect(page).toHaveURL(`/schedules/${SEEDED}`);
  });

  await test.step("3. the page reads the cadence, the clock and the failures", async () => {
    await expect(
      page.getByRole("heading", { name: "Daily cluster report" }),
    ).toBeVisible();
    // The header carries the cadence and the clock as pills; the list below carries the
    // rest.
    const meta = page.getByTestId("schedule-meta");
    await expect(meta).toContainText("Every day at 09:00");
    await expect(meta).toContainText("UTC");
    // A filled Run: it is the action this page exists for.
    await expect(page.getByTestId("schedule-run")).toHaveClass(
      /ant-btn-primary/,
    );
    await expect(
      page.getByRole("columnheader", { name: "Failure reason", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Execution deadline exceeded", { exact: true }),
    ).toBeVisible();

    // The conversation an execution produced is reachable from its row, with one icon
    // rather than a second stacked on it.
    const conversation = page.getByTestId("execution-conversation").first();
    await expect(conversation).toHaveAttribute(
      "href",
      "/agents/6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44/chat",
    );
    await expect(conversation.locator("svg")).toHaveCount(1);
  });

  await test.step("4. the history pages, and page two is its own set of rows", async () => {
    // The history outgrows a page long before the schedule list does, so this is the
    // one table here that has to page at all.
    await page.getByTestId("schedule-history-pages-next").click();
    await expect(page.getByTestId("schedule-history-pages-number")).toContainText("Page 2");
    await expect(page.getByTestId("execution-conversation")).toHaveCount(1);
    await page.getByTestId("schedule-history-pages-prev").click();
    await expect(page.getByTestId("schedule-history-pages-number")).toContainText("Page 1");
  });

  await test.step("5. an execution row expands on a click, but its conversation link navigates", async () => {
    /*
     * The conversation link is the exception that stops this table using antd's
     * `expandRowByClick`: the whole row opens the panel, except where the row holds
     * something separately clickable. The guard silently ceasing to match is what this
     * pins — the click would then unfold the row on the way out.
     */
    const row = rowNamed("Execution deadline exceeded");
    await row.getByRole("cell").filter({ hasText: "Timed out" }).click();
    await expect(page.getByTestId("execution-detail")).toContainText("Original task");
    await expect(page.getByTestId("execution-detail")).toContainText("mock-scheduled-task-1");

    // The expand icon still collapses it, so a keyboard reaches the panel.
    await row.locator(".ant-table-row-expand-icon").click();
    await expect(page.getByTestId("execution-detail")).toBeHidden();

    // And the link navigates rather than expanding. The guard silently ceasing to match
    // is what this pins: the click would then unfold the row on the way out, and only
    // this assertion would notice.
    await row.getByTestId("execution-conversation").click();
    await expect(page).toHaveURL("/agents/6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44/chat");
    await expect(page.getByTestId("execution-detail")).toHaveCount(0);
    await page.goBack();
    await expect(page).toHaveURL(`/schedules/${SEEDED}`);
  });

  await test.step("6. the history opens as a whole and searches the loaded page", async () => {
    // Expand all works on what is on screen, which is why the search sits beside it: the
    // two compose, and "all" after a search means the matches rather than the page.
    const expandAll = page.getByTestId("history-expand-all");
    const panels = page.locator(".ant-table-expanded-row:visible");
    const historyRows = page.locator("tbody tr.ant-table-row");
    await expect(historyRows).toHaveCount(25);

    await expect(expandAll).toHaveText("Expand all");
    await expandAll.click();
    await expect(panels).toHaveCount(25);
    await expect(expandAll).toHaveText("Collapse all");
    await expandAll.click();
    await expect(panels).toHaveCount(0);

    // `mock-scheduled-task-3` is the original task ID, which is only in the panel — so a
    // row matching only inside its panel has to come back opened.
    await page.getByTestId("history-search").fill("mock-scheduled-task-3");
    await expect(historyRows).toHaveCount(1);
    await expect(panels).toHaveCount(1);

    // And a revealed row is still the reader's to close. It was not: `revealed` used to
    // be unioned into the expanded keys on every render, so the click removed the key
    // and the reveal put it straight back.
    await historyRows.first().click();
    await expect(panels).toHaveCount(0);

    // A search matching nothing is not an empty history.
    await page.getByTestId("history-search").fill("nothing matches this");
    await expect(page.getByTestId("schedule-history-no-match")).toBeVisible();
    // The distinction the three ids exist for: a search that matched nothing is not an
    // empty history, and neither is a failed read.
    await expect(page.getByTestId("schedule-history-empty")).toHaveCount(0);
    await page.getByTestId("history-search").fill("");
  });

  await test.step("7. delete sits in its own section at the foot, not in the header", async () => {
    // Asserted by position, not only by presence: the point is that the irreversible
    // control is not one a reader reaches while aiming at Run.
    const danger = page.getByTestId("schedule-danger");
    // Level 3, the same as "Execution history": the two are siblings, not one inside the
    // other.
    await expect(
      danger.getByRole("heading", { level: 3, name: "Danger zone" }),
    ).toBeVisible();
    await expect(danger).toContainText("Deleting this schedule stops future executions.");

    // The header keeps the four reversible controls and none of the destructive one.
    await expect(
      page.getByRole("button", { name: /^(Run|Pause|Resume|Edit|Refresh)$/ }),
    ).toHaveCount(4);
    await expect(page.getByRole("button", { name: /^Delete/ })).toHaveCount(1);
  });

  await test.step("8. an advanced expression survives an edit to another field", async () => {
    // The repeat picker cannot represent every cron expression, so an edit to the prompt
    // must not quietly rewrite the cadence into the nearest thing the picker can draw.
    await page.getByTestId("schedule-edit").click();
    await expect(page).toHaveURL(`/schedules/${SEEDED}/edit`);
    await page.getByTestId("schedule-frequency").click();
    await optionNamed(page, "Custom (advanced)").click();
    await expect(page.getByTestId("schedule-cron")).toHaveValue(
      "0 9 * * *",
    );
    await page.getByTestId("schedule-cron").fill("0 9-17 * * 1-5");
    await page.getByTestId("schedule-submit").click();
    await expect(page).toHaveURL(`/schedules/${SEEDED}`);

    await page.getByTestId("schedule-edit").click();
    await expect(page.getByTestId("schedule-cron")).toHaveValue(
      "0 9-17 * * 1-5",
    );
    await page.getByTestId("schedule-prompt").fill("Preserve business hours.");
    await page.getByTestId("schedule-submit").click();
    await expect(page).toHaveURL(`/schedules/${SEEDED}`);
    await expect(page.getByTestId("schedule-meta")).toContainText(
      "Custom: 0 9-17 * * 1-5",
    );
  });

  await test.step("9. pausing suppresses the cron, and Run still invokes it by hand", async () => {
    // One control whose label flips, so it is driven by id and the label is what gets
    // asserted — by name it would be two different buttons that are the same button.
    await page.getByTestId("schedule-pause").click();
    await expect(page.getByTestId("schedule-pause")).toHaveText("Resume");
    await expect(page.getByTestId("schedule-pause")).toBeEnabled();
    await expect(page.getByTestId("schedule-meta")).toContainText("Paused");

    await page.getByTestId("schedule-run").click();
    await expect(page.getByTestId("schedule-history-pages-number")).toContainText("Page 1");
    await expect(rows.filter({ hasText: "Manual" })).toContainText("Pending");
  });

  await test.step("10. the create form refuses a schedule with no agent to run", async () => {
    await page.getByTestId("schedule-back").click();
    await page.getByTestId("schedules-new").click();
    await expect(page).toHaveURL("/schedules/new");
    // `/schedules/new` is the create page, not a schedule called "new" — the detail
    // route would otherwise swallow it and show an execution history for nothing.
    await expect(
      page.getByRole("heading", { name: "New schedule", exact: true }),
    ).toBeVisible();
    await expect(page.getByTestId("schedule-history")).toHaveCount(0);

    await page.getByTestId("schedule-submit").click();
    await expect(page.getByText("Choose an agent.", { exact: true })).toBeVisible();
  });

  await test.step("11. a read failure is not an empty list", async () => {
    await page.goto("/schedules?mock=error");
    await expect(page.getByTestId("schedules-error")).toContainText(
      "Could not load schedules",
    );
    // "There are none" and "we could not find out" lead a reader to opposite
    // conclusions, and only one of them is true.
    await expect(page.getByTestId("schedules-empty")).toHaveCount(0);
  });

  await test.step("12. and an empty list says so plainly, with nothing to scroll", async () => {
    await page.goto("/schedules?mock=empty");
    await expect(page.getByTestId("schedules-empty")).toBeVisible();

    // The width floor is what the columns need, and an empty table has no columns to
    // fit — reserving it put a scrollbar under the empty state with nowhere to go.
    const overflows = await page
      .locator(".ant-table-content, .ant-table-body")
      .first()
      .evaluate((el) => el.scrollWidth > el.clientWidth);
    expect(overflows).toBe(false);
  });

  await test.step("13. a link held from before a delete still opens, and says what it is", async () => {
    // The executions are retained, so the address is not a 404 — and must not render as
    // a live schedule either, or a reader will try to act on one that is gone.
    await page.goto(`/schedules/${RETIRED}?mock=ok`);
    await expect(page.getByRole("heading", { name: "Retired sweep" })).toBeVisible();
    await expect(page.getByTestId("schedule-deleted-note")).toContainText(
      "Its execution history is retained",
    );
    for (const name of ["Run", "Pause", "Edit"]) {
      await expect(page.getByRole("button", { name, exact: true })).toBeDisabled();
    }
    await expect(page.getByTestId("schedule-meta")).toContainText("Deleted");
  });

  await test.step("14. and it is not offered in the list it was removed from", async () => {
    await page.goto("/schedules?mock=ok");
    await expect(
      page.getByTestId("schedule-link-Retired sweep"),
    ).toHaveCount(0);
  });});
