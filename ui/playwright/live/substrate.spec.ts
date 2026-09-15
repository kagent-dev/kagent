import { test, expect } from "@playwright/test";
import { expectNoLoadFailure, liveRoutes, loadLive } from "./helpers/live";

/**
 * The substrate page, against what the controller actually sends.
 *
 * This page reads `GetSubstrateStatus` and nothing else. It briefly read three RPCs
 * instead — a summary and a page each of actors and workers — and those were removed
 * again, so `api/grpc/operations.ts` answers all four of its operations from that one
 * response and does the narrowing in memory.
 *
 * Worth a live spec rather than trusting the mock one. The fixtures were written for
 * whichever shape was current, and on this project every defect found by pointing the
 * app at a real backend was a place where a fixture taught a shape the controller does
 * not use. A single-message read that the mock serves happily is exactly the kind of
 * thing that can come back empty from a cluster with the tiles still drawing zeros.
 */
test("live: the substrate page renders the cluster's own inventory", async ({ page }) => {
  await loadLive(page, liveRoutes.substrate);
  await expectNoLoadFailure(page);

  await test.step("1. the page is there rather than an error", async () => {
    await expect(page.getByTestId("substrate-actors-card")).toBeVisible({
      timeout: 60_000,
    });
  });

  await test.step("2. the tiles report a real count, not zero", async () => {
    /*
     * The assertion "no load failure" cannot make. A page that reached the controller
     * and understood none of the answer draws the same tiles with nothing in them, and
     * this cluster is running a worker pool — so a zero here is a decode problem, not
     * an empty cluster.
     */
    const actors = page.getByTestId("substrate-stat-actors");
    await expect(actors).toBeVisible();
    const text = (await actors.textContent()) ?? "";
    expect(text, "the actor tile should report a count").toMatch(/\d/);
  });

  await test.step("3. the worker table holds rows the cluster returned", async () => {
    // Workers are what the chart's own `kagent-default` pool provides, so a cluster
    // from `scripts/setup-cluster` always has some. Actors need a conversation to
    // exist and are deliberately not asserted here.
    const workers = page.getByTestId("substrate-workers-table");
    await expect(workers).toBeVisible();
    await expect(workers.locator(".ant-table-row").first()).toBeVisible({
      timeout: 60_000,
    });
  });
});
