import { test, expect } from "../fixtures/test";
import { expectNoLoadFailure, loadApp } from "../helpers/app";
import { liveRoutes } from "./helpers/live";

/**
 * The substrate page, against what the controller actually sends.
 *
 * This page reads three RPCs: `GetSubstrateSummary` for the tiles and the two lists
 * that are inherently small, and a page each from `ListSubstrateActors` and
 * `ListSubstrateWorkers`. It briefly read `GetSubstrateStatus` alone instead, which
 * cannot survive a large cluster — one message of every actor and worker — and that
 * is what this spec exists to keep it away from.
 *
 * Worth a live spec rather than trusting the mock one. The fixtures were written for
 * whichever shape was current, and on this project every defect found by pointing the
 * app at a real backend was a place where a fixture taught a shape the controller does
 * not use. Paging is exactly that kind of thing: the mock pages an array it holds in
 * memory, while the controller passes a token through to ate-api, and only a cluster
 * says whether the token it hands back means what the page thinks it means.
 */
test("live: the substrate page renders the cluster's own inventory", async ({ page }) => {
  await loadApp(page, liveRoutes.substrate);

  await test.step("1. the page is there rather than an error", async () => {
    await expect(page.getByTestId("substrate-actors-card")).toBeVisible({
      timeout: 60_000,
    });
  });

  await test.step("2. the tiles report a real count, not zero", async () => {
    /*
     * The assertion "no load failure" cannot make: a page that understood none of the
     * answer draws the same tiles, with an em-dash where each number goes. Retrying,
     * because that em-dash is also what shows while the read is in flight — read once,
     * this failed reporting "Actors running—" against a cluster that said "0/4" a moment
     * later. Against the mock that gap is a millisecond, so only a cluster showed it.
     */
    await expect(
      page.getByTestId("substrate-stat-actors-value"),
      "the actor tile should report a count",
    ).toHaveText(/\d/, { timeout: 60_000 });
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

    /*
     * Last, and not after the card in step 1: the card is a shell the page draws
     * before it has read anything, so an alert counted then is counted before any read
     * could have failed — and `expectNoLoadFailure` reads once rather than retrying, so
     * it would never look again. Here, three reads have demonstrably landed.
     */
    await expectNoLoadFailure(page);
  });
});
