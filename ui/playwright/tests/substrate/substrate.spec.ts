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
 * screenshotted. What bounds it is the page: the read asks for twenty rows, so twenty
 * is all there is to render. The table used to absorb a page of a hundred by windowing
 * them inside a fixed-height body, which put a scrollbar over the same list the pager
 * moves through — two ways to reach row forty, one of which silently skips rows the
 * other one turns to.
 *
 * So this pins both: the rows are ordered, and the body they sit in does not scroll.
 * The order matters because an unordered list of thousands reshuffles itself on every
 * poll — a row moves under the pointer while it is being read. Nothing on the wire
 * imposes one; ate-api offers no order, so the controller applies it before cutting
 * the page, and this is what says that still happens.
 */
test("substrate: the actor list is ordered, and the page bounds it without a scrollbar", async ({
  page,
}) => {
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

  // Nothing windows the rows any more, so there is no virtual holder to scroll inside.
  await expect(actors.locator(".ant-table-tbody-virtual-holder")).toHaveCount(0);

  // And nothing inside the table scrolls vertically. Asked of every element rather than
  // of the one antd happens to use, because which element that is depends on what
  // `scroll` was given: with a `y` it is `.ant-table-body`, without one there is no such
  // element at all — so naming it is how this passes by finding nothing.
  const scrollers = await actors.evaluate((table) =>
    [table, ...table.querySelectorAll("*")]
      .filter((el) => {
        const overflow = getComputedStyle(el).overflowY;
        return (
          (overflow === "auto" || overflow === "scroll") &&
          el.scrollHeight > el.clientHeight
        );
      })
      .map((el) => `${el.className || el.tagName}: ${el.scrollHeight}px in ${el.clientHeight}px`),
  );
  expect(
    scrollers,
    "the pager moves through the actors; a scrollbar over the same rows is a second way to do it",
  ).toEqual([]);
});

/**
 * Each section narrows on its own, and a match is found wherever it is.
 *
 * Four searches rather than one for the page, because these lists answer four different
 * questions: narrowing the actors to one template must not also empty the table that
 * says what that template is.
 *
 * Two of them are the server's. ate-api offers no filter, so the controller reads every
 * one of its pages and applies the term before cutting this one — which is what makes a
 * match on the ninth page findable at all. The count beside the heading is then the
 * matching total across every page, and that is what this pins: a bare page length
 * presented as the result is how a reader concludes their cluster holds one actor.
 */
test("substrate: each list narrows on its own, and a match is found wherever it is", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  const actorsCard = page.getByTestId("substrate-actors-card");
  const templatesCard = page.getByTestId("substrate-templates-card");
  const actorsTable = page.getByTestId("substrate-actors-table");

  await test.step("1. the term narrows the list, and the count is of every match", async () => {
    await page.getByTestId("substrate-actors-search").locator("input").fill("7f21");

    await expect(actorsTable).toContainText("actor-7f21");
    await expect(actorsTable).not.toContainText("actor-9c03");
    // One match in the scope, not "one of the eight on this page": the total came back
    // with the answer, counted over everything the filter matched.
    await expect(actorsCard).toContainText("1");
  });

  await test.step("2. a narrowed list never reads as the size of the cluster", async () => {
    // The tile keeps the cluster's own size on screen, counted by GetSubstrateSummary
    // rather than from the rows. A reader who searched and found one actor must not
    // conclude their cluster is running one.
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
    // "anywhere in this scope" is a claim only a server-side filter can make, and the
    // sentence that stops a reader taking an empty page for an empty cluster.
    await expect(actorsTable).toContainText("No actors match your search");
    await expect(actorsTable).toContainText("anywhere in this scope");
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
 * All four tables sort the same way, and the paged two order the whole inventory.
 *
 * The actor and worker columns once carried a header of this page's own — a button
 * around the title, an arrow beside it, nothing outside those few words to click —
 * written that way to avoid antd's `sorter`, which reorders the rows the table was
 * handed. The concern was right and the remedy was not: the page ended up with two
 * tables that sort by clicking a header and two that sort by clicking the words inside
 * one, which is a page a reader has to learn twice.
 *
 * What the paged columns declare is `sorter: true` — antd's header, with no comparator
 * behind it — so the whole cell is the target and the table still reorders nothing
 * itself. A click becomes the next read, and the controller walks every ate-api page to
 * order all of them before this one is cut. That is the claim the strip beneath each
 * table makes, and the half this pins: if a comparator is ever handed to one of these
 * tables the order would hold over the page alone, and these assertions are what would
 * object.
 */
test("substrate: every table sorts through the same header, and the paged two order the lot", async ({
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

  await test.step("2. the actors' order covers every row, and cycles back to the default", async () => {
    const actors = page.getByTestId("substrate-actors-table");
    const order = page.getByTestId("substrate-actors-order");
    // The order the answer says it applied, not the one the control asked for.
    await expect(order).toContainText("status, then actor");

    // The header cell, not the words in it: clicking the cell is what a reader does on
    // the two tables above, and this is the assertion that the same click works here.
    const header = actors.locator("th").first();

    await header.click();
    await expect(order).toContainText("Sorted across the whole inventory: actor, ascending");
    await expect.poll(() => firstColumn(actors)).toEqual(
      await firstColumn(actors).then((ids) => [...ids].sort()),
    );

    await header.click();
    await expect(order).toContainText("Sorted across the whole inventory: actor, descending");

    // antd's third click clears the sort, which for a read that always arrives ordered
    // means the order it falls back to rather than no order at all.
    await header.click();
    await expect(order).toContainText("status, then actor");
  });

  await test.step("3. and the workers' the same", async () => {
    const order = page.getByTestId("substrate-workers-order");
    await expect(order).toContainText("pool, then pod");

    await page.getByTestId("substrate-workers-table").locator("th").nth(1).click();
    await expect(order).toContainText("Sorted across the whole inventory: pool, ascending");
  });

  await test.step("4. the actors are grouped by status, in an order nobody asked for", async () => {
    await loadPage(page, routes.substrate, { title: "Substrate" });
    await expectSettled(page);

    // Stated rather than asked for: ate-api returns actors in whatever order it holds
    // them, so the same actor would appear somewhere different on every poll. Something
    // has to impose an order, and that something is the read rather than the table.
    // The status cell itself rather than a pattern over the row: a list of words to
    // match is a list to keep up to date, and a state it has not been told about reads
    // as empty and sorts first, which looks like the order being wrong.
    const statuses = await page
      .getByTestId("substrate-actors-table")
      .locator(".ant-table-row")
      .evaluateAll((rows) =>
        rows.map((row) => (row.querySelectorAll(".ant-table-cell")[1]?.textContent ?? "").trim()),
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
 * written as `tr:hover > td`, which a virtual table has neither of. So the two tables
 * here that were then windowed went on hovering while every other static table in the
 * app had stopped, and this page offered both behaviours at once.
 *
 * All four are still checked. They are one kind of markup now that nothing here is
 * virtual, but the rules that suppress the highlight stayed class-based, and a rule
 * written for the markup of the day is what caused this in the first place.
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
