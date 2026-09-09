import type { Locator } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { expectSettled, loadPage, routes } from "../../helpers/app";
import { paint, settledPaint } from "../../helpers/style";

/**
 * Substrate — the inventory, its scope, and the three ways the read can answer.
 *
 * The page used to carry a banner reading "worker pool and actor inventory is not
 * available here… comes from a status endpoint this UI's data layer does not expose yet".
 * That was true when it was written and quietly stopped being true: the endpoint, the
 * client method, the hook and the types were all in place, and only the page had not been
 * told.
 *
 * So this covers what it now shows — four sections, all of them the substrate's own — and,
 * more importantly, that the read's three answers stay distinct. `enabled: false` is a
 * deployment without an ate-api endpoint, which is ordinary rather than broken, and is
 * said in the two tables it actually applies to. `ateApiError` means the Kubernetes-derived
 * halves are complete while the runtime ones may be partial, which is a warning *beside*
 * the data rather than an error instead of it. A page that flattened those into one message
 * would tell an operator their substrate was broken when it was merely switched off.
 *
 * The fixture is built for exactly this: `enabled: true` with an `ateApiError` set, two
 * worker pools across two namespaces, two templates — one Ready in `kagent`, one Pending in
 * `platform` — eight actors and two workers, one of the workers holding nothing. The crashed
 * actor sits last in the fixture and first once sorted, which is what makes the ordering
 * testable at all.
 */

test("substrate: the inventory renders, and partial runtime data says so", async ({
  page,
}) => {
  await test.step("1. the stale banner is gone", async () => {
    await loadPage(page, routes.substrate, { title: "Substrate" });
    await expectSettled(page);

    // The exact claim that outlived its own truth. Asserted by its words rather than a
    // test id, because the point is that this sentence is not on the page.
    await expect(page.getByText("not available here")).toHaveCount(0);
  });

  await test.step("2. the summary counts both halves of each ratio", async () => {
    // A bare count answers the wrong question: one template ready is good news or bad
    // depending on how many there are. Both numbers, or the tile is not worth its space.
    await expect(page.getByTestId("substrate-stat-pools-value")).toHaveText("2");
    await expect(page.getByTestId("substrate-stat-templates-value")).toHaveText("1/2");
    // Two running of eight: the rest are crashed, deleting, paused, resuming, suspended
    // and snapshotting, which is exactly the case a bare count would hide.
    await expect(page.getByTestId("substrate-stat-actors-value")).toHaveText("2/8");
    await expect(page.getByTestId("substrate-stat-workers-value")).toHaveText("1/2");
    await expect(page.getByTestId("substrate-stat-scope-value")).toHaveText("all");
  });

  await test.step("2b. the bar is the shape of the cluster, not just its running tally", async () => {
    const bar = page.getByTestId("substrate-actor-status-counts");
    await expect(bar).toBeVisible();

    // One segment per actor, so the bar is counted rather than estimated, and ordered by
    // the status with everything parked pushed to the end — the grey tail is the last
    // thing on the bar, not something cutting the active part in half.
    await expect(bar.locator("[data-tone]")).toHaveCount(8);
    await expect(
      bar.locator("[data-tone]").evaluateAll((els) => els.map((el) => el.getAttribute("data-tone"))),
    ).resolves.toEqual([
      "danger",
      "warning",
      "progress",
      "healthy",
      "healthy",
      "progress",
      "idle",
      "idle",
    ]);

    // The whole breakdown from anywhere on the bar, rather than one label per segment: a
    // reader wanting the shape of the cluster should not have to hover it a piece at a time.
    await bar.hover();
    const tip = page.locator(".ant-tooltip");
    for (const line of [
      "Crashed Actors: 1",
      "Deleting Actors: 1",
      "Resuming Actors: 1",
      "Running Actors: 2",
      "Snapshotting Actors: 1",
      "Paused Actors: 1",
      "Suspended Actors: 1",
    ]) {
      await expect(tip).toContainText(line);
    }

    // The legend says the same numbers without a pointer at all, which is what a reader
    // looking at a screenshot or a printed page has.
    const legend = page.getByTestId("substrate-actor-status-counts-legend");
    await expect(legend).toContainText("Crashed: 1");
    await expect(legend).toContainText("Running: 2");
    await expect(legend).toContainText("Suspended: 1");

    // Every state the controller can report, so a reader learns the vocabulary from the
    // page rather than from waiting for something to go wrong.
    await expect(legend).toContainText("Pausing: 0");
    await expect(legend).toContainText("Unknown: 0");
    // `ACTOR_STATE_CRASHED` and a vocabulary entry of `Crashed` are the same status, and
    // keying the legend on the wire value listed it twice — once at zero.
    await expect(legend.getByText(/^Crashed: /)).toHaveCount(1);

    // The same summary as text, because hovering needs a pointer and neither a screen
    // reader nor a keyboard has one. Colour is never carrying this alone.
    await expect(bar).toHaveAttribute(
      "aria-label",
      "Actor status. Crashed Actors: 1, Deleting Actors: 1, Resuming Actors: 1, Running Actors: 2, Snapshotting Actors: 1, Paused Actors: 1, Suspended Actors: 1",
    );
  });

  await test.step("3. the worker pools the sandboxes run on", async () => {
    const pools = page.getByTestId("substrate-pools-table");
    await expect(pools).toBeVisible();
    await expect(pools).toContainText("kagent/default-pool");
    await expect(pools).toContainText("platform/gpu-pool");
    // The image tag, which is what an operator checks against a release.
    await expect(pools).toContainText("ateom:1.4.0");
  });

  await test.step("4. the templates actors are cut from", async () => {
    const templates = page.getByTestId("substrate-templates-table");
    await expect(templates).toBeVisible();
    await expect(templates).toContainText("kagent/coder-template");
    await expect(templates).toContainText("platform/external-template");

    // The golden actor, beneath the name: it is the snapshot every new actor of this
    // template is cut from, and the one identifier worth carrying beside the name.
    await expect(templates).toContainText("golden: actor-golden-001");

    // The rest of what decides where and how a template runs.
    await expect(templates).toContainText("standard");
    await expect(templates).toContainText("pool=default-pool");
    await expect(templates).toContainText("openclaw");

    // Both phases, and coloured by what they mean rather than all alike: a Ready template
    // reads as healthy, a Pending one does not.
    await expect(templates).toContainText("Ready");
    await expect(templates).toContainText("Pending");
    await expect(
      templates.locator("[data-tone]").filter({ hasText: "Ready" }),
    ).toHaveAttribute("data-tone", "healthy");
  });

  await test.step("5. the actors placed right now, and the pods holding them", async () => {
    const actors = page.getByTestId("substrate-actors-table");
    await expect(actors).toBeVisible();
    await expect(actors).toContainText("actor-7f21");
    await expect(actors).toContainText("kagent/coder-template");
    // The pod, with its IP appended — the two facts an operator needs to go and look.
    await expect(actors).toContainText("kagent/ateom-default-pool-0");
    await expect(actors).toContainText("10.42.1.19");

    // Both wire constants are read to the operator as words — a humaniser that only knew
    // `CRASHED` would leave the other one showing the controller's vocabulary.
    await expect(actors).not.toContainText("ACTOR_STATE_");
    await expect(actors).toContainText("Deleting");
    await expect(
      actors.locator("[data-tone]").filter({ hasText: "Crashed" }),
    ).toHaveAttribute("data-tone", "danger");
  });

  await test.step("6. the workers, and no claim about which actor is on them", async () => {
    const workers = page.getByTestId("substrate-workers-table");
    await expect(workers).toBeVisible();
    await expect(workers).toContainText("kagent/ateom-default-pool-0");
    await expect(workers).toContainText("default-pool");
    await expect(workers).toContainText("10.42.1.19");

    /*
     * No Actor column, and this pins its absence. ate-api's `Worker` carries capacity
     * and allocation and no actor reference: the controller has nothing to fill that
     * column from, so it read "idle" for every worker on every real cluster and looked
     * populated only here, against a fixture that had invented the field. How much of
     * the fleet is busy is a tile, counted once by the summary.
     */
    await expect(workers).not.toContainText("actor-7f21");
    await expect(workers).not.toContainText("idle");
    await expect(page.getByTestId("substrate-stat-workers")).toContainText("1/2");
  });

  await test.step("7. partial runtime data is a warning beside the data, not instead of it", async () => {
    // The fixture sets `ateApiError`. Both must be true at once: the warning is shown, and
    // the tables it qualifies are still there — that is the whole distinction.
    await expect(page.getByTestId("substrate-partial")).toBeVisible();
    await expect(page.getByTestId("substrate-inventory-error")).toHaveCount(0);
    await expect(page.getByTestId("substrate-actors-table")).toContainText("actor-7f21");
  });
});

/**
 * The scope control.
 *
 * `GetSubstrateStatusRequest` takes a namespace and an empty one means every namespace the
 * controller watches, so the page offers both. The test is not that a dropdown opens: it is
 * that choosing a namespace narrows what is read — the fixture backend filters the way the
 * controller filters — and that the choice is in the address, so a link to what somebody is
 * looking at is a link to what they are looking at.
 */
test("substrate: the scope narrows what is read, and is carried in the URL", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  await test.step("1. it opens on every watched namespace", async () => {
    await expect(page.getByTestId("substrate-namespace")).toContainText(
      "All watched namespaces",
    );
    await expect(page.getByTestId("substrate-pools-table")).toContainText("kagent/default-pool");
    await expect(page.getByTestId("substrate-pools-table")).toContainText("platform/gpu-pool");
  });

  await test.step("2. choosing one namespace narrows every section", async () => {
    await page.getByTestId("substrate-namespace").click();
    // The one place this suite reaches for an antd class name. The visible dropdown is a
    // portal outside the app's own markup, and `getByRole("option")` also matches the
    // zero-sized accessibility listbox rc-select keeps inside the combobox — which can
    // never be clicked, so a role query here waits for actionability until it times out.
    await page
      .locator(".ant-select-item-option")
      .filter({ hasText: /^kagent$/ })
      .click();

    await expect(page).toHaveURL(/namespace=kagent/);
    await expect(page.getByTestId("substrate-stat-scope-value")).toHaveText("kagent");

    const pools = page.getByTestId("substrate-pools-table");
    await expect(pools).toContainText("kagent/default-pool");
    await expect(pools).not.toContainText("platform/gpu-pool");

    const templates = page.getByTestId("substrate-templates-table");
    await expect(templates).toContainText("coder-template");
    await expect(templates).not.toContainText("external-template");
  });

  await test.step("3. the scope is the address, so a link to it opens on it", async () => {
    await loadPage(page, `${routes.substrate}?namespace=platform`, { title: "Substrate" });
    await expectSettled(page);

    await expect(page.getByTestId("substrate-namespace")).toContainText("platform");
    await expect(page.getByTestId("substrate-stat-scope-value")).toHaveText("platform");
    await expect(page.getByTestId("substrate-pools-table")).toContainText("platform/gpu-pool");
  });

  await test.step("4. an empty section says why it is empty", async () => {
    // Every worker in the fixture is in `kagent`, so this scope has none — and the
    // sentence has to distinguish "ate-api has nothing here" from "there is no ate-api",
    // which are different facts and only one of them is something to go and fix.
    const workers = page.getByTestId("substrate-workers-table");
    await expect(workers).toContainText("ate-api reported no worker assignments");
    await expect(workers).not.toContainText("not configured");
  });
});

/**
 * A controller with no ate-api endpoint.
 *
 * `enabled: false` is a deployment choice, not a fault, and the page has to say so in the
 * two places it applies without dressing it up as a failure anywhere. The `empty` scenario
 * is exactly this: `enabled` false and every list absent.
 */
test("substrate: an unconfigured ate-api is explained, not reported as broken", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { scenario: "empty", title: "Substrate" });
  await expectSettled(page);

  // Said by the two tables it applies to, not by a tile: a tile is for a number that
  // moves, and this one read `connected` above ate-api's own timeout banner.
  await expect(page.getByTestId("substrate-stat-ateapi")).toHaveCount(0);
  await expect(page.getByTestId("substrate-inventory-error")).toHaveCount(0);
  await expect(page.getByTestId("substrate-partial")).toHaveCount(0);

  // The bar keeps its track and says why it is empty. Removing it instead would move the
  // table under a reader at the moment a cluster drained, which is the moment they are
  // watching it.
  await expect(page.getByTestId("substrate-actor-status-counts")).toBeVisible();
  await expect(page.getByTestId("substrate-actor-status-counts").locator("[data-tone]")).toHaveCount(0);
  await expect(page.getByTestId("substrate-actor-status-counts-empty")).toHaveText(
    "ate-api is not configured, so there are no actors to show.",
  );

  // The two runtime sections name the setting to change. The two Kubernetes ones do not —
  // they are empty for an unrelated reason, and saying "ate-api" over them would send an
  // operator to fix the wrong thing.
  await expect(page.getByTestId("substrate-actors-table")).toContainText(
    "substrate-ate-api-endpoint",
  );
  await expect(page.getByTestId("substrate-workers-table")).toContainText(
    "ate-api, which is not configured",
  );
  await expect(page.getByTestId("substrate-pools-table")).toContainText(
    "Create one in the cluster",
  );
  // A template appears when a harness and an agent template are paired, which is
  // what creates one — not the legacy resource this used to name, which the API does
  // not serve.
  await expect(page.getByTestId("substrate-templates-table")).toContainText(
    "harness and an agent template",
  );
});

/*
 * The bar draws a segment per actor with a 6px floor and does not wrap, so its width is
 * set by the cluster rather than by the window: eight actors want 69px and eighty want
 * 717px, which is more than the track has at 1024 — where the sidebar expands and leaves
 * it 686px. Unbounded, the bar forced its own container wider and took the page with it.
 *
 * The track rather than the page, deliberately. These tables carry a horizontal minimum
 * of their own (`scroll.x`), so the page scrolls sideways below about 1100px whether or
 * not there is a single actor on it — asserting on the page would be asserting on that
 * instead, and would pass or fail for reasons this bar has no say in.
 */
test("substrate: the status bar stays inside its track, whatever the window", async ({
  page,
}) => {
  for (const width of [375, 768, 1024, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await loadPage(page, routes.substrate, { title: "Substrate" });
    await expectSettled(page);

    const bar = page.getByTestId("substrate-actor-status-counts");
    await expect(bar).toBeVisible();

    const track = await bar.evaluate((el) => ({
      client: el.clientWidth,
      scroll: el.scrollWidth,
    }));
    expect(
      track.scroll,
      `at ${width}px the bar wants ${track.scroll}px in a ${track.client}px track`,
    ).toBeLessThanOrEqual(track.client);
  }
});

/**
 * The actor list is the one thing on this page whose length the cluster chooses.
 *
 * A real controller answered with 34,356 actors, and rendered in full that came to a
 * 1.4-million-pixel page which took seconds to become interactive and could not be
 * screenshotted. So the table is windowed and its body bounded, and this covers both
 * halves of that: only a window of rows reaches the DOM, and the page stays a fixed
 * size regardless.
 *
 * The order is checked here too, because an unordered list of thousands reshuffles
 * itself on every poll — a row moves under the pointer while it is being read. Nothing
 * on the wire imposes one: ate-api pages and offers no order, so `SubstratePage` sorts
 * the page it was handed before antd sees it, and this is what says that still happens.
 */
test("substrate: the actor list is ordered, windowed, and bounded", async ({ page }) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  const actors = page.getByTestId("substrate-actors-table");

  // Sorted by status, then by id, and the fixture lists them in none of that order.
  expect(await firstColumn(actors)).toEqual([
    "actor-0aa1",
    "actor-2e40",
    "actor-5d17",
    "actor-8b91",
    "actor-3b55",
    "actor-7f21",
    "actor-9c03",
    "actor-c3f5",
  ]);

  // Windowed: antd renders rows into a virtual holder rather than a plain tbody, which
  // is what keeps a list of thousands off the page.
  await expect(
    actors.locator(".ant-table-tbody-virtual-holder"),
  ).toHaveCount(1);

  // Bounded: the body scrolls inside itself instead of growing the document.
  const height = await actors
    .locator(".ant-table-tbody-virtual-holder")
    .evaluate((el) => el.getBoundingClientRect().height);
  expect(height).toBeLessThanOrEqual(520);
});

/**
 * Each section narrows on its own, and says what its search reached.
 *
 * Four searches rather than one for the page, because these lists answer four
 * different questions: narrowing the actors to one template must not also empty the
 * table that says what that template is.
 *
 * Two of them reach further than the other two, and the point of this test is that the
 * page says which is which. Worker pools and actor templates arrive whole in the
 * summary, so searching them searches all of them. Actors and workers arrive one page
 * at a time — `ListSubstrateActors` passes a token through to ate-api, which offers
 * paging and no filter — so those two searches reach one page.
 *
 * A page-scoped search is only honest while it is labelled. "No matches" over a search
 * that looked at a hundred of four hundred thousand rows is how a reader concludes an
 * actor does not exist, so the empty state names what it searched, the heading
 * separates "1 of 8 on this page" from "8 of 4,312", and the note beneath the table
 * says it outright. Those three sentences are the assertions here; if a filter is ever
 * pushed into the API, they are what should change with it.
 */
test("substrate: each list narrows on its own, and says what its search reached", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  const actorsCard = page.getByTestId("substrate-actors-card");
  const templatesCard = page.getByTestId("substrate-templates-card");
  const actorsTable = page.getByTestId("substrate-actors-table");

  await test.step("1. the term narrows the page, and the heading says it was the page", async () => {
    await page.getByTestId("substrate-actors-search").locator("input").fill("7f21");

    await expect(actorsTable).toContainText("actor-7f21");
    await expect(actorsTable).not.toContainText("actor-9c03");

    // "1 of 8 on this page", not "1 of 8". The second reads as one match in the whole
    // scope, which is a claim this search cannot make.
    await expect(actorsCard).toContainText("1 of 8 on this page");
  });

  await test.step("2. a narrowed list never reads as the size of the cluster", async () => {
    // The tile is what keeps the cluster's own size on screen, counted server-side by
    // `GetSubstrateSummary` rather than from the rows. A reader who searched and found
    // one actor must not conclude their cluster is running one.
    await expect(page.getByTestId("substrate-stat-actors")).toContainText("/8");
  });

  await test.step("3. and only that card: the other lists are left alone", async () => {
    await expect(templatesCard).toContainText("coder-template");
  });

  await test.step("4. a search matching nothing says so, and says where it looked", async () => {
    await page
      .getByTestId("substrate-actors-search")
      .locator("input")
      .fill("no-such-actor");
    // The second sentence is the one that matters. Without it a reader takes "no
    // matches" for "no such actor", and the row they are looking for is on page nine.
    await expect(actorsTable).toContainText("No actors on this page match your search");
    await expect(actorsTable).toContainText("Other pages are not searched");
  });

  await test.step("5. the whole-list searches say nothing of the kind, because they reach the whole list", async () => {
    await page
      .getByTestId("substrate-templates-search")
      .locator("input")
      .fill("no-such-template");
    const templatesTable = page.getByTestId("substrate-templates-table");
    await expect(templatesTable).toContainText("No actor templates match your search");
    await expect(templatesTable).not.toContainText("this page");
  });
});

/** The first cell of every rendered row, which for both paged tables is its identity. */
async function firstColumn(table: Locator) {
  return table
    .locator(".ant-table-row")
    .evaluateAll((rows) =>
      rows.map((row) => row.querySelector(".ant-table-cell")?.textContent?.trim() ?? ""),
    );
}

/**
 * All four tables sort the same way, and the paged two say how far that reaches.
 *
 * The actor and worker columns once carried a header of this page's own — a button
 * around the title, an arrow beside it, nothing outside those few words to click —
 * written that way to avoid antd's `sorter`, which reorders the rows the table was
 * handed. The concern was right and the remedy was not: the page ended up with two
 * tables that sort by clicking a header and two that sort by clicking the words inside
 * one, which is a page a reader has to learn twice.
 *
 * Then they declared `sorter: true` — antd's header with no comparator behind it — and
 * a click became the next read, which ordered every actor in the cluster before the
 * browser sliced out a page. Honest, and it could not survive a large cluster: that
 * read answers with one message, and 410,110 actors is roughly 43MB against gRPC's
 * 16MB ceiling.
 *
 * So the reads page, and a comparator is what is left. All four tables sort through
 * antd's own header, and the two paged ones sort the rows they hold. What this pins is
 * that they say so: the strip beneath each names its reach, and no strip anywhere
 * claims an order over the cluster. If sort is ever pushed into ate-api and back out
 * through `ListSubstrateActors`, this is the assertion that should change with it.
 */
test("substrate: every table sorts through the same header, and the paged two say how far", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  await test.step("1. every table's headers are antd's own sort controls", async () => {
    for (const testId of [
      "substrate-pools-table",
      "substrate-templates-table",
      "substrate-actors-table",
      "substrate-workers-table",
    ]) {
      const headers = page.getByTestId(testId).locator("th");
      await expect(headers.first()).toBeVisible();
      const sortable = await headers.evaluateAll((cells) =>
        cells.filter((cell) => cell.className.includes("column-has-sorters")).length,
      );
      const total = await headers.count();
      expect(
        sortable,
        `${testId}: every column sorts, and through the header rather than a control inside it`,
      ).toBe(total);
    }
  });

  await test.step("2. the paged tables say what their sort reaches, and never claim the cluster", async () => {
    for (const testId of ["substrate-actors-order", "substrate-workers-order"]) {
      const note = page.getByTestId(testId);
      await expect(note).toContainText("Sorting and search apply to this page only");
      // The sentence this replaced. It was true of a read that fetched every row; it
      // is not true of a page, and nothing here may drift back to it.
      await expect(note).not.toContainText("whole inventory");
    }
  });

  await test.step("3. a header click reorders the rows in hand, and cycles back to the default", async () => {
    const actors = page.getByTestId("substrate-actors-table");
    // The header, not the words in it: clicking the cell is what a reader does on the
    // two tables above, and this is the assertion that the same click works here.
    const header = actors.locator("th").first();

    // `aria-sort` rather than a caption: it is the state a screen reader is given, so
    // asserting it covers the reader who cannot see the arrow.
    await expect(header).not.toHaveAttribute("aria-sort", /.*/);
    const byStatus = await firstColumn(actors);

    await header.click();
    await expect(header).toHaveAttribute("aria-sort", "ascending");
    // The rows themselves, because a header that says ascending over rows that never
    // moved is the failure worth catching. Relative to what arrived rather than a list
    // written out here, so growing the fixture does not silently stop testing this.
    await expect.poll(() => firstColumn(actors)).toEqual([...byStatus].sort());

    await header.click();
    await expect(header).toHaveAttribute("aria-sort", "descending");
    await expect.poll(() => firstColumn(actors)).toEqual([...byStatus].sort().reverse());

    /*
     * No loading state through any of that, because there is no read behind it. This is
     * the whole of what paging bought a reader: the sort used to be part of the SWR key,
     * so every header click re-fetched the entire inventory to reorder rows the browser
     * was already holding.
     */
    await expect(actors.locator(".ant-spin")).toHaveCount(0);

    // antd's third click clears the sort, which for rows the page ordered before antd
    // saw them means the order they fall back to rather than no order at all.
    await header.click();
    await expect(header).not.toHaveAttribute("aria-sort", /.*/);
    await expect.poll(() => firstColumn(actors)).toEqual(byStatus);
  });

  await test.step("4. and the workers' the same", async () => {
    const header = page.getByTestId("substrate-workers-table").locator("th").nth(1);
    await header.click();
    await expect(header).toHaveAttribute("aria-sort", "ascending");
  });

  await test.step("5. the actors are grouped by status, in an order nobody asked for", async () => {
    // Stated rather than asked for: ate-api returns actors in whatever order it holds
    // them, so the same actor would appear somewhere different on every poll. Something
    // has to impose an order, and with nothing on the wire to ask for one it is the
    // page, over the rows it was handed.
    await loadPage(page, routes.substrate, { title: "Substrate" });
    await expectSettled(page);

    const statuses = await page
      .getByTestId("substrate-actors-table")
      .locator(".ant-table-row")
      .evaluateAll((rows) =>
        rows.map(
          (row) =>
            row.textContent?.match(
              /Crashed|Deleting|Paused|Resuming|Running|Snapshotting|Suspended/,
            )?.[0] ?? "",
        ),
      );
    expect(statuses).toEqual([...statuses].sort());
  });
});

/**
 * Nothing on this page is a link, and nothing on it lights up under the pointer.
 *
 * A row that changes colour on hover reads as a click target. None of these four is one:
 * there is no page for an actor, a worker, a pool or a template to open. The app has a
 * rule for exactly this — hover is opt-in through `clickable-table-row` — and it was
 * written as `tr:hover > td`, which a virtual table has neither of. So the two windowed
 * tables here went on hovering while every other static table in the app had stopped,
 * and this page offered both behaviours at once.
 *
 * Both bodies are checked because they are different markup: the pools and templates are
 * a real `table`, the actors and workers are divs from antd's virtual list.
 */
test("substrate: rows nobody can click do not light up under the pointer", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  for (const testId of [
    "substrate-pools-table",
    "substrate-templates-table",
    "substrate-actors-table",
    "substrate-workers-table",
  ]) {
    const row = page.getByTestId(testId).locator(".ant-table-row").first();
    await expect(row).toBeVisible();
    const cell = row.locator(".ant-table-cell").first();

    const atRest = (await paint(cell)).background;
    await row.hover();
    /*
     * That the hover landed is asserted before what it painted. antd marks the hovered
     * row's cells whatever the app then does with them, so this separates "the rule
     * suppressed the highlight" from "the pointer never arrived" — which the colour
     * comparison alone cannot do, and which a fixed wait on a loaded box invites.
     */
    await expect(cell).toHaveClass(/ant-table-cell-row-hover/);
    // Waited out rather than polled: the claim is that nothing happens, and there is no
    // event for a transition that never starts. See `helpers/style`.
    const hovered = (await settledPaint(cell)).background;

    expect(hovered, `${testId}: a row that cannot be clicked must not look clickable`).toBe(
      atRest,
    );
  }
});
