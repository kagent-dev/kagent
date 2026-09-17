import { test, expect } from "../fixtures/test";
import { loadApp, throwawayName } from "../helpers/app";
import { tick } from "../helpers/controls";
import { sweepQuietly } from "../helpers/cleanup";
import {
  LIFECYCLE_TIMEOUT,
  appeared,
  optionNamed,
  pressUntil,
} from "../helpers/resource";
import { liveRoutes } from "./helpers/live";

/**
 * A schedule survives a reload — which is the one claim the fixtures cannot answer.
 *
 * The journey itself is in `shared/schedules/schedules.spec.ts` and runs against both
 * backends. What is left here is the part that has to be live: everything a create
 * reports back could be the form showing itself its own draft, and only re-reading from
 * a backend that actually stores can tell the two apart. The fixture backend keeps
 * writes in the page's own memory, so a reload there starts a backend that has never
 * heard of the schedule — the one move it cannot survive.
 *
 * The values are chosen to be the ones a controller drops or rounds: a fractional
 * timeout stored as seconds plus nanos, a named IANA zone, and a weekday set.
 *
 * Kept paused: execution is covered by the Go scheduling E2Es with a controlled model.
 */

const CREATED = throwawayName("schedule");

/*
 * A journey's budget, not a page's. This creates on a cluster, reloads twice and deletes
 * in a `finally` — the same shape as every `shared/` journey, which all take
 * `LIFECYCLE_TIMEOUT`. Left on the global live budget it is the tightest one in the run,
 * and a kill mid-cleanup leaves a real Schedule behind.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("live: a schedule's configuration survives a reload", async ({ page }) => {
  let detailURL: string | undefined;

  try {
    await test.step("1. a schedule is created with the values most easily lost", async () => {
      await loadApp(page, liveRoutes.schedules);
      await page.getByTestId("schedules-new").click();

      await page.getByTestId("schedule-agent").click();
      const agent = optionNamed(page).first();
      await expect(agent, "the cluster offered no agents to schedule").toBeVisible({
        timeout: 30_000,
      });
      await agent.click();

      await page.getByTestId("schedule-name").fill(CREATED);
      await page.getByTestId("schedule-frequency").click();
      await pressUntil(optionNamed(page, "Weekly"), () =>
        expect(page.getByTestId("schedule-days")).toBeVisible(),
      );
      for (const day of ["Tuesday", "Wednesday", "Thursday", "Friday"]) {
        await tick(page.getByLabel(day, { exact: true }));
      }
      await page.getByTestId("schedule-time").fill("09:00");
      await page.getByTestId("schedule-timezone").locator("input").fill("America/New_York");
      await page.keyboard.press("Escape");
      await page.getByTestId("schedule-prompt").fill("Report cluster health.");
      await page.getByTestId("schedule-timeout").fill("90.001");
      await page.getByTestId("schedule-enabled").uncheck();

      await page.getByTestId("schedule-submit").click();
      await expect(page.getByRole("heading", { name: CREATED, exact: true })).toBeVisible({
        timeout: 60_000,
      });
      detailURL = page.url();
    });

    await test.step("2. a reload reads it back from the controller, unchanged", async () => {
      // The whole spec. Everything above could be the page showing itself what it just
      // sent; only a re-read says the controller stored it.
      await page.reload();

      await expect(page.getByTestId("schedule-pause")).toHaveText("Resume", {
        timeout: 60_000,
      });
      await expect(page.getByTestId("schedule-meta")).toContainText("Weekdays at 09:00");
      await expect(page.getByTestId("schedule-meta")).toContainText("America/New_York");
      // `90.001` is stored as seconds plus nanos, so a controller that kept only the
      // seconds would read back "90 seconds" here.
      await expect(page.getByTestId("schedule-detail")).toContainText("90.001 seconds");
      await expect(page.getByTestId("schedule-detail")).toContainText(
        "Report cluster health.",
      );
    });

    await test.step("3. and so does an edit", async () => {
      await page.getByTestId("schedule-edit").click();
      await expect(page.getByTestId("schedule-timeout")).toHaveValue("90.001", {
        timeout: 60_000,
      });
      await page.getByTestId("schedule-prompt").fill("Report unhealthy workloads only.");
      await page.getByTestId("schedule-submit").click();
      await expect(page.getByRole("heading", { name: CREATED, exact: true })).toBeVisible({
        timeout: 60_000,
      });

      await page.reload();
      const detail = page.getByTestId("schedule-detail");
      await expect(detail).toContainText("Report unhealthy workloads only.", {
        timeout: 60_000,
      });
      // The update did not quietly reset what it was not asked to change.
      await expect(detail).toContainText("90.001 seconds");
    });
  } finally {
    // A real schedule on a real cluster, and this spec never deletes one in the body —
    // so every run, passing or not, leaves through here.
    if (detailURL) {
      // Captured, because the closure below outlives the narrowing of a `let`.
      const detail = detailURL;
      await sweepQuietly(CREATED, async () => {
        await page.goto(detail);
        const remove = page
          .getByTestId("schedule-danger")
          .getByRole("button", { name: `Delete schedule ${CREATED}`, exact: true });
        // Waited for, not counted once: `goto` resolves on load and the detail read has
        // not landed, so the danger zone is not drawn yet. See `appeared`.
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
    }
  }
});
