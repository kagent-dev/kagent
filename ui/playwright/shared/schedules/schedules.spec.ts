import { test, expect } from "../../fixtures/test";
import { READ_TIMEOUT, loadApp, throwawayName } from "../../helpers/app";
import { tick } from "../../helpers/controls";
import { sweepQuietly } from "../../helpers/cleanup";
import {
  LIFECYCLE_TIMEOUT,
  appeared,
  optionNamed,
  pressUntil,
} from "../../helpers/resource";

/**
 * A schedule created, read back, changed and deleted — on either backend.
 *
 * Kept paused throughout: execution is covered by the Go scheduling E2Es with a
 * controlled model, and a suite that waited for a real agent to answer would be
 * measuring the model rather than the schedule.
 *
 * **No reload anywhere in it, deliberately.** The fixture backend keeps writes in the
 * page's own memory, so a reload starts a backend that has never heard of the schedule
 * — which is why `live/schedules.spec.ts` exists alongside this and owns exactly that
 * claim. Here every read is a click-through, which is what a reader does anyway.
 */

const CREATED = throwawayName("schedule");

test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

/** Where this run's schedule lives, so the hook below can remove it. */
let detailURL: string | undefined;

/*
 * Cleanup in a hook, not in the body's `finally`.
 *
 * A timed-out test is the likeliest live failure — a controller that never reconciles —
 * and it is exactly the one a `finally` cannot clean up after: Playwright has closed the
 * page by then, so every call in it throws. Measured, with a four-second test: the
 * `finally` was refused with "Target page, context or browser has been closed" while
 * this hook still drove the page. Hooks get their own budget, which is the point.
 */
test.afterEach(async ({ page }) => {
  if (detailURL === undefined) return;
  const detail = detailURL;
  detailURL = undefined;
  await sweepQuietly(CREATED, async () => {
    await page.goto(detail);
    const remove = page
      .getByTestId("schedule-danger")
      .getByRole("button", { name: `Delete schedule ${CREATED}`, exact: true });
    // Waited for, not counted once: `goto` resolves on load and the detail read has not
    // landed, so the danger zone is not drawn yet. See `appeared`.
    if (await appeared(remove)) {
      await remove.click();
      await pressUntil(
        page
          .getByRole("dialog", { name: `Delete schedule ${CREATED}?`, exact: true })
          .getByRole("button", { name: "Delete", exact: true }),
        () => expect(page).toHaveURL(/\/schedules(\?|$)/),
      );
    }
  });
});

test("schedules: one is created, read, changed and deleted", async ({ page }) => {
  await test.step("1. the form offers the backend's own agents", async () => {
    await loadApp(page, "/schedules");
    await page.getByTestId("schedules-new").click();
    await expect(page).toHaveURL(/\/schedules\/new(\?|$)/);

    await page.getByTestId("schedule-agent").click();
    // Whichever agent this install has: which one has nothing to do with the claim.
    const agent = optionNamed(page).first();
    await expect(agent, "no agents were offered to schedule").toBeVisible({
      timeout: 30_000,
    });
    await agent.click();
  });

  await test.step("2. a weekly, zoned, fractionally-timed schedule is described", async () => {
    await page.getByTestId("schedule-name").fill(CREATED);

    await page.getByTestId("schedule-frequency").click();
    // Pressed until the cadence actually changes: the weekday checkboxes only exist
    // once the frequency is weekly, so a dropdown click swallowed by the animation
    // leaves the next line waiting for controls that are never coming.
    await pressUntil(optionNamed(page, "Weekly"), () =>
      expect(page.getByTestId("schedule-days")).toBeVisible(),
    );
    // Monday is already on, so these four make it the whole working week — which the
    // app states back as "Weekdays", and which is the reading asserted below.
    for (const day of ["Tuesday", "Wednesday", "Thursday", "Friday"]) {
      await tick(page.getByLabel(day, { exact: true }));
    }

    await page.getByTestId("schedule-time").fill("09:00");
    // The time zone is an AutoComplete, so its id is on the wrapper and the caret goes
    // in the input inside it. Escape dismisses the zone list, which otherwise sits
    // over the fields below.
    await page.getByTestId("schedule-timezone").locator("input").fill("America/New_York");
    await page.keyboard.press("Escape");

    await page.getByTestId("schedule-prompt").fill("Report cluster health.");
    // A fractional timeout, because it is the value a backend most easily rounds off.
    await page.getByTestId("schedule-timeout").fill("90.001");

    await page.getByTestId("schedule-enabled").uncheck();
    await expect(page.getByTestId("schedule-enabled-note")).toContainText(
      "will not run automatically after it is created",
    );
  });

  await test.step("3. creating it lands on its own page, showing what was asked for", async () => {
    await page.getByTestId("schedule-submit").click();
    await expect(page.getByRole("heading", { name: CREATED, exact: true })).toBeVisible({
      timeout: READ_TIMEOUT,
    });
    detailURL = page.url();
    await expect(page).toHaveURL(/\/schedules\/[0-9a-f-]+(\?|$)/);

    // Created paused, so the one control whose label flips offers to resume it.
    await expect(page.getByTestId("schedule-pause")).toHaveText("Resume");
    await expect(page.getByTestId("schedule-meta")).toContainText("Weekdays at 09:00");
    await expect(page.getByTestId("schedule-meta")).toContainText("America/New_York");
    // The fractional second survived the round trip rather than being floored to 90.
    await expect(page.getByTestId("schedule-detail")).toContainText("90.001 seconds");
  });

  await test.step("4. the edit form opens on the stored values, not on defaults", async () => {
    await page.getByTestId("schedule-edit").click();
    await expect(page.getByTestId("schedule-time")).toHaveValue("09:00", {
      timeout: READ_TIMEOUT,
    });
    await expect(page.getByTestId("schedule-timeout")).toHaveValue("90.001");
    await expect(page.getByTestId("schedule-timezone").locator("input")).toHaveValue(
      "America/New_York",
    );
    await expect(page.getByTestId("schedule-enabled")).not.toBeChecked();
    // Both ends of the weekday set, so a picker that kept only the last day chosen
    // would not pass on one assertion.
    await expect(page.getByLabel("Monday", { exact: true })).toBeChecked();
    await expect(page.getByLabel("Friday", { exact: true })).toBeChecked();
  });

  await test.step("5. an edit is saved and read back", async () => {
    await page.getByTestId("schedule-prompt").fill("Report unhealthy workloads only.");
    await page.getByTestId("schedule-submit").click();
    await expect(page.getByRole("heading", { name: CREATED, exact: true })).toBeVisible({
      timeout: READ_TIMEOUT,
    });

    const detail = page.getByTestId("schedule-detail");
    await expect(detail).toContainText("Report unhealthy workloads only.", {
      timeout: READ_TIMEOUT,
    });
    // And the update did not quietly reset what it was not asked to change.
    await expect(detail).toContainText("90.001 seconds");
  });

  await test.step("6. deleting asks in a modal, and confirming leaves for the list", async () => {
    const remove = page
      .getByTestId("schedule-danger")
      .getByRole("button", { name: `Delete schedule ${CREATED}`, exact: true });
    await remove.click();

    // Pressed until it takes: a dropped Delete reports as "the page never navigated"
    // rather than as a missed click. See `pressUntil`.
    await pressUntil(
      page
        .getByRole("dialog", { name: `Delete schedule ${CREATED}?`, exact: true })
        .getByRole("button", { name: "Delete", exact: true }),
      () => expect(page).toHaveURL(/\/schedules(\?|$)/),
    );
    detailURL = undefined;

    await expect(page.getByRole("link", { name: CREATED, exact: true })).toHaveCount(0, {
      timeout: READ_TIMEOUT,
    });
  });
});
