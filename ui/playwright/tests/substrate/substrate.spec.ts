import type { Locator } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { expectSettled, loadPage, routes } from "../../helpers/app";
import { paint, settledPaint } from "../../helpers/style";

/**
 * Substrate inventory, scope, and successful, empty, and failed reads.
 * An ateApiError warns that runtime data may be partial while Kubernetes data is complete.
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
    // and suspending, which is exactly the case a bare count would hide.
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
      "Suspending Actors: 1",
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
      "Actor status. Crashed Actors: 1, Deleting Actors: 1, Resuming Actors: 1, Running Actors: 2, Suspending Actors: 1, Paused Actors: 1, Suspended Actors: 1",
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
    await expect(templates).toContainText("gvisor");
    await expect(templates).toContainText("pool=default-pool");
    await expect(templates.getByRole("columnheader", { name: "Harness", exact: true })).toHaveCount(0);

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

/** Namespace and atespace filters are independent and survive sharing the URL. */
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

  await test.step("2. choosing one namespace narrows Kubernetes resources", async () => {
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
    await expect(page.getByTestId("substrate-stat-scope-value")).toHaveText("K8s: kagent; ATE: all");

    const pools = page.getByTestId("substrate-pools-table");
    await expect(pools).toContainText("kagent/default-pool");
    await expect(pools).not.toContainText("platform/gpu-pool");

    const templates = page.getByTestId("substrate-templates-table");
    await expect(templates).toContainText("coder-template");
    await expect(templates).toContainText("external-template");
    await expect(page.getByTestId("substrate-stat-actors-value")).toHaveText("2/8");
  });

  await test.step("an atespace filters actors by their own identity, independently of Kubernetes", async () => {
    const atespace = page.getByRole("searchbox", { name: "ATE atespace", exact: true });
    await atespace.fill("team-a");
    await atespace.press("Enter");
    await expect(page).toHaveURL(/atespace=team-a/);
    await expect(page.getByTestId("substrate-stat-actors-value")).toHaveText("1/1");
    await expect(page.getByTestId("substrate-actors-table")).toContainText("team-a/actor-7f21");
    await expect(page.getByTestId("substrate-actors-table")).toContainText("kagent/coder-template");
    await expect(page.getByTestId("substrate-templates-table")).not.toContainText("coder-template");
    await expect(page.getByTestId("substrate-pools-table")).toContainText("kagent/default-pool");
    await expect(page.getByTestId("substrate-stat-workers-value")).toHaveText("1/2");
    await page.reload();
    await expect(page.getByRole("searchbox", { name: "ATE atespace", exact: true })).toHaveValue("team-a");
    await expect(page.getByTestId("substrate-stat-actors-value")).toHaveText("1/1");
    await atespace.fill("");
    await atespace.press("Enter");
    await expect(page).not.toHaveURL(/atespace=/);
    await expect(page.getByTestId("substrate-stat-actors-value")).toHaveText("2/8");
  });

  await test.step("3. the scope is the address, so a link to it opens on it", async () => {
    await loadPage(page, `${routes.substrate}?namespace=platform`, { title: "Substrate" });
    await expectSettled(page);

    await expect(page.getByTestId("substrate-namespace")).toContainText("platform");
    await expect(page.getByTestId("substrate-stat-scope-value")).toHaveText("K8s: platform; ATE: all");
    await expect(page.getByTestId("substrate-pools-table")).toContainText("platform/gpu-pool");
  });

  await test.step("4. an empty section says why it is empty", async () => {
    // Every worker in the fixture is in `kagent`, so this scope has none — and the
    // sentence has to distinguish "ate-api has nothing here" from "there is no ate-api",
    // which are different facts and only one of them is something to go and fix.
    const workers = page.getByTestId("substrate-workers-table");
    await expect(workers).toContainText("No worker assignments in this namespace scope on this page");
    await expect(workers).not.toContainText("not configured");
  });
});

test("substrate: an empty inventory is shown without errors", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { scenario: "empty", title: "Substrate" });
  await expectSettled(page);

  await expect(page.getByTestId("substrate-stat-ateapi")).toHaveCount(0);
  await expect(page.getByTestId("substrate-inventory-error")).toHaveCount(0);
  await expect(page.getByTestId("substrate-partial")).toHaveCount(0);

  // The bar keeps its track and says why it is empty. Removing it instead would move the
  // table under a reader at the moment a cluster drained, which is the moment they are
  // watching it.
  await expect(page.getByTestId("substrate-actor-status-counts")).toBeVisible();
  await expect(page.getByTestId("substrate-actor-status-counts").locator("[data-tone]")).toHaveCount(0);
  await expect(page.getByTestId("substrate-actor-status-counts-empty")).toHaveText(
    "No actors in this scope.",
  );

  await expect(page.getByTestId("substrate-actors-table")).toContainText(
    "No actors on this page.",
  );
  await expect(page.getByTestId("substrate-workers-table")).toContainText(
    "No worker assignments in this namespace scope on this page.",
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

/** Preserve upstream order and keep the page itself as the vertical scroll container. */
test("substrate: the actor list preserves upstream order without a nested scrollbar", async ({
  page,
}) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);

  const actors = page.getByTestId("substrate-actors-table");

  // Preserve the order supplied by the upstream fixture.
  expect(await firstColumn(actors)).toEqual([
    "team-a/actor-7f21",
    "kagent/actor-9c03",
    "kagent/actor-0aa1",
    "kagent/actor-3b55",
    "kagent/actor-5d17",
    "kagent/actor-2e40",
    "kagent/actor-8b91",
    "kagent/actor-c3f5",
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

test("substrate: searches apply only to the complete configuration lists", async ({ page }) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);
  await expect(page.getByRole("textbox", { name: "Search actors", exact: true })).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "Search workers", exact: true })).toHaveCount(0);
  await page.getByRole("textbox", { name: "Search actor templates", exact: true }).fill("coder");
  const templates = page.getByTestId("substrate-templates-table");
  await expect(templates).toContainText("coder-template");
  await expect(templates).not.toContainText("external-template");
  await expect(page.getByTestId("substrate-actors-card")).toContainText("8 on this page");
  await expect(page.getByTestId("substrate-stat-actors")).toContainText("/8");
});

/** The first cell of every rendered row, which for both paged tables is its identity. */
async function firstColumn(table: Locator) {
  return table
    .locator(".ant-table-row")
    .evaluateAll((rows) =>
      rows.map((row) => row.querySelector(".ant-table-cell")?.textContent?.trim() ?? ""),
    );
}

test("substrate: only complete lists offer column sorting", async ({ page }) => {
  await loadPage(page, routes.substrate, { title: "Substrate" });
  await expectSettled(page);
  for (const name of ["actors", "workers"]) {
    const table = page.getByTestId(`substrate-${name}-table`);
    await expect(table.locator("th.ant-table-column-has-sorters")).toHaveCount(0);
    await expect(page.getByTestId(`substrate-${name}-order`)).toHaveCount(0);
  }
  for (const name of ["pools", "templates"]) {
    const headers = page.getByTestId(`substrate-${name}-table`).getByRole("columnheader");
    for (const header of await headers.all()) {
      await expect(header).toHaveClass(/ant-table-column-has-sorters/);
    }
  }
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
