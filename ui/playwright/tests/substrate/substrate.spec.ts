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
 * `platform` — three actors and two workers, one of the workers holding nothing. The third
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
    // Two running of four: one of the fixture's actors is `Failed` and another
    // `Snapshotting`, which is exactly the case a bare count would hide.
    await expect(page.getByTestId("substrate-stat-actors-value")).toHaveText("2/4");
    await expect(page.getByTestId("substrate-stat-workers-value")).toHaveText("1/2");
    await expect(page.getByTestId("substrate-stat-ateapi-value")).toHaveText("connected");
    await expect(page.getByTestId("substrate-stat-scope-value")).toHaveText("all");
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
  });

  await test.step("6. the workers, including the one holding nothing", async () => {
    const workers = page.getByTestId("substrate-workers-table");
    await expect(workers).toBeVisible();
    await expect(workers).toContainText("kagent/ateom-default-pool-0");
    await expect(workers).toContainText("default-pool");
    await expect(workers).toContainText("actor-7f21");
    // "idle" and not a dash: a worker with no actor on it is available, which is a state
    // worth reading, where a dash says only that a cell is empty.
    await expect(workers).toContainText("idle");
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

  await expect(page.getByTestId("substrate-stat-ateapi-value")).toHaveText("off");
  await expect(page.getByTestId("substrate-inventory-error")).toHaveCount(0);
  await expect(page.getByTestId("substrate-partial")).toHaveCount(0);

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
 * itself on every poll — a row moves under the pointer while it is being read.
 */
test("substrate: the actor list is ordered, windowed, and bounded", async ({ page }) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  const actors = page.getByTestId("substrate-actors-table");

  // Sorted by status, then by id. `Failed` precedes `Running` precedes `Snapshotting`,
  // and the fixture lists them in none of that order.
  const ids = await actors.locator(".ant-table-row").evaluateAll((rows) =>
    rows.map((row) => row.querySelector(".ant-table-cell")?.textContent?.trim() ?? ""),
  );
  expect(ids).toEqual(["actor-0aa1", "actor-3b55", "actor-7f21", "actor-9c03"]);

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
 * Each section narrows on its own, and a match is found wherever it is.
 *
 * Four searches rather than one for the page, because these lists answer four
 * different questions: narrowing the actors to one template must not also empty the
 * table that says what that template is.
 *
 * The count beside each heading reports both numbers while a search is active. A bare
 * count under a search box is how a reader concludes their cluster has one actor.
 *
 * These searches used to be the server's, and the title used to say so. They are the
 * browser's again: `GetSubstrateSummary`, `ListSubstrateActors` and
 * `ListSubstrateWorkers` were removed, `GetSubstrateStatus` answers all four lists from
 * one message, and `api/grpc/operations.ts` filters it in memory. The property being
 * asserted did not change — a match is still found wherever it is — but the reason it
 * holds did, from "the server searched everything" to "the browser has everything". See
 * `playwright/DEFERRED.md` for what that costs and when it stops being true.
 */
test("substrate: each list narrows on its own, and a match is found wherever it is", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  const actorsCard = page.getByTestId("substrate-actors-card");
  const templatesCard = page.getByTestId("substrate-templates-card");
  const actorsTable = page.getByTestId("substrate-actors-table");

  await test.step("1. the term narrows the list, and finds a row anywhere in it", async () => {
    // Honest only because the read holds every row. Applied to a page of them it would
    // search that page, and a match on page nine would read on screen as "no matches",
    // which is worse than no search at all — so if this read ever pages or truncates,
    // this search has to go with it.
    await page.getByTestId("substrate-actors-search").locator("input").fill("7f21");

    await expect(actorsTable).toContainText("actor-7f21");
    await expect(actorsTable).not.toContainText("actor-9c03");
  });

  await test.step("2. a narrowed list never reads as the size of the cluster", async () => {
    // The count beside the heading is now the *matching* total, so the tile is what
    // keeps the cluster's own size on screen. A reader who searched and found one
    // actor must not conclude their cluster is running one.
    await expect(page.getByTestId("substrate-stat-actors")).toContainText("/4");
  });

  await test.step("3. and only that card: the other lists are left alone", async () => {
    await expect(templatesCard).toContainText("coder-template");
  });

  await test.step("4. a search matching nothing says so, and says where it looked", async () => {
    await page
      .getByTestId("substrate-actors-search")
      .locator("input")
      .fill("no-such-actor");
    // "anywhere in this scope" rather than "on this page" — a claim the page can only
    // make because every row in the scope is in the browser to be searched.
    await expect(actorsTable).toContainText("No actors match your search");
    await expect(actorsCard).toContainText("anywhere in this scope");
  });
});

/**
 * All four tables sort the same way, and the paged two say honestly what they sorted.
 *
 * The actor and worker columns used to carry a header of this page's own: a button around
 * the title, an arrow beside it, and nothing outside those few words to click. It was
 * written that way to avoid antd's `sorter`, which reorders the rows the table was handed
 * — and one page out of 410,110 reordered is not the cluster sorted.
 *
 * The concern was right and the remedy was not: the page ended up with two tables that
 * sort by clicking a header and two that sort by clicking the words inside one, which is
 * a page a reader has to learn twice. What the columns declare now is `sorter: true` —
 * antd's header, with no comparator behind it — so the whole cell is the target and the
 * chevrons show the direction, while the table still reorders nothing itself. A click
 * becomes the next read, which orders every row before this page gets a slice of it.
 *
 * Not the server, which takes a namespace and nothing else: the ordering is applied in
 * `localPage` over the whole inventory. That is still the honest claim at this size —
 * the order holds over the cluster rather than over the hundred rows on screen — and
 * what the strip beside each table has to say, which is the half this pins. If a
 * comparator is ever handed to one of these tables, the order would hold over the page
 * alone and these assertions are what would object.
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
    const order = page.getByTestId("substrate-actors-order");
    await expect(order).toContainText("status, then actor");

    // The header, not the words in it: clicking the cell is what a reader does on the
    // two tables above, and this is the assertion that the same click works here.
    const header = page.getByTestId("substrate-actors-table").locator("th").first();
    await header.click();
    await expect(order).toContainText("Sorted across the whole inventory: actor, ascending");

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
    // Stated rather than asked for: ate-api returns actors in whatever order it holds
    // them, so the same actor would appear somewhere different on every poll. Something
    // has to impose an order, and that something is the read rather than the table.
    const statuses = await page
      .getByTestId("substrate-actors-table")
      .locator(".ant-table-row")
      .evaluateAll((rows) =>
        rows.map((row) => row.textContent?.match(/Failed|Running|Snapshotting/)?.[0] ?? ""),
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
