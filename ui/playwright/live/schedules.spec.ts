import { test, expect } from "../fixtures/test";
import { liveRoutes, loadLive, throwawayName } from "./helpers/live";
import { tick } from "../helpers/controls";
import { optionNamed, pressUntil } from "../helpers/resource";

/**
 * A schedule's configuration, round-tripped through a real controller. Kept paused —
 * execution is covered by the Go scheduling E2Es with a controlled model.
 *
 * The values are the ones a backend drops or rounds: a fractional timeout, a named IANA
 * zone, a weekday set. A fixture hands back whatever it was given, so the mock suite
 * cannot fail on a controller that truncates `90.001` to `90`; this can.
 *
 * It also drove `getByRole("dialog")` for an editor that is a page, and named CI's
 * `smoke` agent rather than `setup-cluster.sh`'s — so it could only pass where nothing
 * ran it. Both found the first time it was pointed at a cluster.
 */
test("live: schedule configuration persists through the browser and controller", async ({
  page,
}) => {
  const name = throwawayName("schedule");
  let detailURL: string | undefined;

  try {
    await test.step("1. the form offers the cluster's own agents", async () => {
      await loadLive(page, liveRoutes.schedules);
      await page.getByTestId("schedules-new").click();
      await expect(page).toHaveURL(/\/schedules\/new$/);

      await page.getByTestId("schedule-agent").click();
      // Whichever agent this install has: which one has nothing to do with the claim.
      const agent = optionNamed(page).first();
      await expect(agent, "the cluster offered no agents to schedule").toBeVisible({
        timeout: 30_000,
      });
      await agent.click();
    });

    await test.step("2. a weekly, zoned, fractionally-timed schedule is described", async () => {
      await page.getByTestId("schedule-name").fill(name);

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
      await page.getByTestId("schedule-timeout").fill("90.001");

      await page.getByTestId("schedule-enabled").uncheck();
      await expect(page.getByTestId("schedule-enabled-note")).toContainText(
        "will not run automatically after it is created",
      );
    });

    await test.step("3. creating it reaches the controller and lands on its page", async () => {
      await page.getByTestId("schedule-submit").click();
      await expect(page.getByRole("heading", { name, exact: true })).toBeVisible({
        timeout: 60_000,
      });
      detailURL = page.url();
      await expect(page).toHaveURL(/\/schedules\/[0-9a-f-]+$/);
    });

    await test.step("4. a reload reads it back from the cluster, unchanged", async () => {
      // The reload is the point. Everything above could be the form showing itself its
      // own draft; only a re-read says the controller stored it.
      await page.reload();

      // Created paused, so the one control whose label flips offers to resume it.
      await expect(page.getByTestId("schedule-pause")).toHaveText("Resume", {
        timeout: 60_000,
      });
      await expect(page.getByTestId("schedule-meta")).toContainText("Weekdays at 09:00");
      await expect(page.getByTestId("schedule-meta")).toContainText("America/New_York");
      // The fractional second survived. `90.001` is stored as seconds plus nanos, so a
      // controller that kept only the seconds would read back "90 seconds" here.
      await expect(page.getByTestId("schedule-detail")).toContainText("90.001 seconds");
      await expect(page.getByTestId("schedule-detail")).toContainText(
        "Report cluster health.",
      );
    });

    await test.step("5. the edit form opens on the stored values, not on defaults", async () => {
      await page.getByTestId("schedule-edit").click();
      await expect(page.getByTestId("schedule-time")).toHaveValue("09:00", {
        timeout: 60_000,
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

    await test.step("6. an edit is saved and read back", async () => {
      await page.getByTestId("schedule-prompt").fill("Report unhealthy workloads only.");
      await page.getByTestId("schedule-submit").click();
      await expect(page.getByRole("heading", { name, exact: true })).toBeVisible({
        timeout: 60_000,
      });

      await page.reload();
      await expect(page.getByTestId("schedule-detail")).toContainText(
        "Report unhealthy workloads only.",
        { timeout: 60_000 },
      );
      // And the update did not quietly reset what it was not asked to change.
      await expect(page.getByTestId("schedule-detail")).toContainText("90.001 seconds");
    });
  } finally {
    /*
     * A real schedule on a real cluster, so a run that dies midway takes it with it.
     * Through the UI, the app speaking gRPC-Web with no REST endpoint to call instead.
     */
    if (detailURL) {
      await page.goto(detailURL);
      const remove = page
        .getByTestId("schedule-danger")
        .getByRole("button", { name: `Delete schedule ${name}`, exact: true });
      await remove.click();
      // Pressed until it takes: a dropped Delete reports as "the page never navigated"
      // rather than as a missed click. See `pressUntil`.
      await pressUntil(
        page.getByRole("dialog", { name: `Delete schedule ${name}?`, exact: true })
          .getByRole("button", { name: "Delete", exact: true }),
        () => expect(page).toHaveURL(/\/schedules$/),
      );
    }
  }
});
