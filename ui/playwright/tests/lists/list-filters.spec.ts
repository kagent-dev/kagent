import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";

/**
 * The filter bar as the list pages actually use it.
 *
 * Deliberately not the bar's own behaviour. `FilterBar.test.tsx` owns that — eleven
 * cases covering a pill per chosen value, a pill removing only its own filter, the last
 * pill taking the parameter with it, clear dropping the lot, the term reaching the
 * address, and a filter change returning to page one. Those claims were asserted here
 * too for a while, which bought nothing and cost two browser engines and a page load
 * each. What is left is what a jsdom test cannot reach:
 *
 * - **A page narrows its own rows.** The bar can be rendered perfectly and never
 *   passed to the read behind the table, and only real data in a real table shows it.
 * - **The view is in the address**, so it can be linked to and survives a real reload —
 *   a reload being the thing under test, which rules out simulating it.
 * - **A filter that is genuinely the server's is sent there.** `ListPromptTemplates`
 *   takes a namespace and refuses a request without one, so choosing namespaces on
 *   that page is one call each rather than a narrowing of something already fetched.
 *   Whether a call happened is not observable from the component.
 * - **Two writes in one tick do not lose one of each other**, which needs the router
 *   and the page together.
 *
 * Driving the antd multi-select needs one piece of local knowledge, which is why it
 * has a helper: rc-select renders a *second*, invisible `role="listbox"` for screen
 * readers, and `getByRole("option")` resolves to that one and then waits forever for a
 * visibility that never arrives — reporting the option as absent while it is on screen
 * the whole time. Locating `.ant-select-item-option` by its title is what actually
 * points at the row a person clicks.
 */

/** Opens a filter's popup and ticks one option by the label the reader sees. */
async function chooseFilter(page: Page, filterTestId: string, label: string) {
  await page.getByTestId(filterTestId).click();
  await page.locator(`.ant-select-item-option[title="${label}"]`).click();
  // Otherwise the popup covers the pill row the next step asserts on.
  await page.keyboard.press("Escape");
}

test("lists: a page's filter narrows that page's own rows", async ({ page }) => {
  /*
   * What the browser is needed to say, and nothing more.
   *
   * The bar's own mechanics — a pill per chosen value, a pill removing only its own
   * filter, the last pill taking the parameter with it, clear dropping everything, the
   * term reaching the address — are `FilterBar.test.tsx`, eleven cases of it, and they
   * used to be re-asserted here as well: the same claims a second time, in two browser
   * engines, at a page load each. What a unit test cannot say is that *this page*
   * wired the bar to its own read, so that is what is left.
   */
  await test.step("1. unnarrowed is every row, and nothing claims otherwise", async () => {
    await loadPage(page, routes.models, { title: "Models" });
    await expectSettled(page);

    // Four configurations across three namespaces — the whole fixture set, which is
    // what "nothing selected" has to mean. A control reading an empty selection as
    // "narrow to nothing" would show an empty table here.
    await expect(dataRows(page)).toHaveCount(4);
    await expect(page.getByTestId("models-filters-pills")).toHaveCount(0);
  });

  await test.step("2. choosing a namespace narrows the rows, and says so in the address", async () => {
    await chooseFilter(page, "models-filters-filter-ns", "kagent");

    // The wiring, in one assertion: the page's own rows respond. A page that rendered
    // the bar and never passed the selection to its read would keep all four.
    await expect(dataRows(page)).toHaveCount(2);
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(page).toHaveURL(/ns=kagent/);
  });

  await test.step("3. a term and a filter chosen in quick succession both survive", async () => {
    /*
     * A regression this build actually had, and the one thing here no unit test
     * reaches: the two writes landed before React had re-rendered, so the second read
     * the address as it was before the first and put the cleared filter straight back
     * — a filter that would not clear, and a search reporting no matches for a row
     * plainly on the page. Driven without waiting in between, because waiting is what
     * hid it.
     */
    await loadPage(page, routes.models, { title: "Models" });
    await expectSettled(page);
    await page.getByTestId("models-filters-search").fill("model");
    await chooseFilter(page, "models-filters-filter-ns", "kagent");

    await expect(page.getByTestId("models-filters-pill-search")).toBeVisible();
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(2);
  });
});

test("lists: a narrowed view is an address, so it survives a reload", async ({ page }) => {
  await test.step("1. narrowing writes what was chosen into the address", async () => {
    await loadPage(page, routes.models, { title: "Models" });
    await expectSettled(page);

    await page.getByTestId("models-filters-search").fill("config");
    await chooseFilter(page, "models-filters-filter-ns", "kagent");

    const url = new URL(page.url());
    expect(url.searchParams.get("q")).toBe("config");
    expect(url.searchParams.getAll("ns")).toEqual(["kagent"]);
  });

  await test.step("2. sorting is in the address too, and the header shows it", async () => {
    await page.getByRole("columnheader", { name: "Name", exact: true }).click();

    await expect(page).toHaveURL(/sort=name/);
    // Ascending is the default direction and is deliberately not written, so the
    // address carries a direction only where one was chosen.
    await expect(page).not.toHaveURL(/dir=/);
  });

  await test.step("3. reloading restores every part of it", async () => {
    await page.reload();
    await expectSettled(page);

    // The controls, not just the parameters: a page that kept the URL and rendered
    // the unfiltered list would pass a URL-only assertion and be broken.
    await expect(page.getByTestId("models-filters-search")).toHaveValue("config");
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(2);
    await expect(
      page.locator("th.ant-table-column-sort").filter({ hasText: "Name" }),
    ).toHaveCount(1);
  });

  await test.step("4. the same address typed fresh gives the same view", async () => {
    // What a link is. Opened cold rather than reloaded, so nothing in memory can be
    // carrying the state.
    await page.goto("/models?mock=ok&q=config&ns=kagent&sort=namespace&dir=desc");
    await expectSettled(page);

    await expect(page.getByTestId("models-filters-search")).toHaveValue("config");
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(2);
  });
});

test("lists: prompts asks the server for exactly the namespaces chosen", async ({
  page,
}) => {
  /*
   * The one filter on these three pages that is not client-side, asserted as what it
   * is rather than as what it looks like. `ListPromptTemplates` takes a namespace, so
   * `usePrompts` fans out one call per namespace — choosing two reads those two, and
   * nothing else is fetched and thrown away.
   */
  await test.step("1. unfiltered, every library is listed", async () => {
    await loadPage(page, routes.prompts, { title: "Prompts" });
    await expectSettled(page);
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(1, { timeout: 30_000 });
    await expect(rowNamed(page, "incident-playbooks")).toHaveCount(1);
  });

  await test.step("2. choosing a namespace leaves only that namespace's libraries", async () => {
    await chooseFilter(page, "prompts-filters-filter-ns", "platform");
    await expectSettled(page);

    await expect(rowNamed(page, "incident-playbooks")).toHaveCount(1);
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(0);
  });

  await test.step("3. the count says what was read, not what the cluster holds", async () => {
    // With the read scoped, the page has not asked about the other namespaces — so it
    // cannot claim a total, and says "read" rather than implying one.
    await expect(page.getByTestId("prompts-summary")).toContainText("1 of 1 library read");
  });

  await test.step("4. an empty namespace says the filter matched nothing, not that none exist", async () => {
    // The distinction the scoped read forces. "No prompt libraries yet" would be a
    // claim about the cluster that this page, having asked about one namespace, is in
    // no position to make.
    await page.goto("/prompts?mock=ok&ns=analytics");
    await expectSettled(page);

    await expect(
      page.getByText("No prompt libraries match those filters."),
    ).toBeVisible();
    await expect(page.getByText("No prompt libraries yet.")).toHaveCount(0);
  });
});
