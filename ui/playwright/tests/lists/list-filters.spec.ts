import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";

/**
 * The filter bar, the URL state behind it, and what the three list pages claim about
 * where their narrowing happens.
 *
 * These are the properties the shared machinery exists for, and each of them replaces
 * something a page was previously doing worse:
 *
 * - **Selecting no namespaces means every namespace.** The old pattern was a separate
 *   "all namespaces" toggle beside a single-select — two controls answering one
 *   question, able to disagree. An empty multi-select has one state.
 * - **The choices are visible without opening the control.** A trigger reading
 *   "2 selected" hides which two, and "why can I not see the row I am looking for" is
 *   the question a filtered list most often provokes.
 * - **The view is in the address**, so it can be linked to and survives a reload.
 * - **The pages say where the narrowing happens.** All three of these RPCs return the
 *   whole list — `ListModelConfigs` and `ListToolServers` take an empty request, and
 *   `ListPromptTemplates` takes only a namespace — so a search in the browser searches
 *   every row. That is the fact the note on each page states, and it is what makes a
 *   client-side control honest here where it would not be on the substrate page's
 *   paged tables.
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

test("lists: no namespaces chosen means every namespace, and each choice becomes a pill", async ({
  page,
}) => {
  await test.step("1. the page opens unnarrowed, with no pills at all", async () => {
    await loadPage(page, routes.models, { title: "Models" });
    await expectSettled(page);

    // Four configurations across three namespaces — the whole fixture set, which is
    // what "nothing selected" has to mean. A control that read an empty selection as
    // "narrow to nothing" would show an empty table here.
    await expect(dataRows(page)).toHaveCount(4);
    await expect(page.getByTestId("models-filters-pills")).toHaveCount(0);
    await expect(page.getByTestId("models-filters-pill-clear")).toHaveCount(0);
  });

  await test.step("2. choosing one namespace narrows the list and raises one pill", async () => {
    await chooseFilter(page, "models-filters-filter-ns", "kagent");

    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toContainText(
      "Namespace: kagent",
    );
    await expect(rowNamed(page, "default-model-config")).toHaveCount(1);
    await expect(rowNamed(page, "ollama-local")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(2);
  });

  await test.step("3. a second namespace adds to the first rather than replacing it", async () => {
    // The whole reason for a multi-select. A single-select answers "which one
    // namespace"; a reader comparing two namespaces has to be able to ask for both.
    await chooseFilter(page, "models-filters-filter-ns", "platform");

    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(page.getByTestId("models-filters-pill-ns-platform")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(3);
    await expect(page.getByTestId("models-summary")).toContainText("3 of 4");
  });

  await test.step("4. a second filter narrows further and keeps its own pill", async () => {
    // Two different filters at once is what the bar takes definitions for. A
    // component built around namespaces could not do this at all.
    await chooseFilter(page, "models-filters-filter-provider", "OpenAI");

    await expect(page.getByTestId("models-filters-pill-provider-OpenAI")).toContainText(
      "Provider: OpenAI",
    );
    await expect(dataRows(page)).toHaveCount(1);
    await expect(rowNamed(page, "default-model-config")).toHaveCount(1);
  });

  await test.step("5. clicking a pill removes that filter and leaves the rest alone", async () => {
    await page.getByTestId("models-filters-pill-provider-OpenAI").click();

    await expect(page.getByTestId("models-filters-pill-provider-OpenAI")).toHaveCount(0);
    // The two namespace pills are untouched: removing one choice is not a reset.
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(page.getByTestId("models-filters-pill-ns-platform")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(3);
  });

  await test.step("6. the last pill of a filter takes the parameter with it", async () => {
    /*
     * Removed back to back, with no wait between them.
     *
     * Each removal used to compute the remainder from the render it was drawn in, so
     * two clicks landing in the same frame both worked from the same list: the second
     * wrote back the namespace the first had just taken out, and a pill survived being
     * clicked. `no wait` is the assertion — pausing here would pass either way.
     */
    await page.getByTestId("models-filters-pill-ns-platform").click({ noWaitAfter: true });
    await page.getByTestId("models-filters-pill-ns-kagent").click({ noWaitAfter: true });

    // Not `?ns=`, which would read as "narrowed to nothing" — the address has to
    // become the address of the unfiltered list again, or a link to "everything" and a
    // link to "nothing" would look the same.
    await expect(page).not.toHaveURL(/ns=/);
    await expect(page.getByTestId("models-filters-pills")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(4);
  });
});

test("lists: the search term is a filter too, and clearing means everything", async ({
  page,
}) => {
  await test.step("1. models has a search box, which it did not before", async () => {
    // The page's only way to find a configuration used to be reading the table.
    await loadPage(page, routes.models, { title: "Models" });
    await expectSettled(page);
    await expect(page.getByTestId("models-filters-search")).toBeVisible();
  });

  await test.step("2. a term narrows the list and appears as its own pill", async () => {
    await page.getByTestId("models-filters-search").fill("haiku");

    // Matched on the model rather than the name, which is the point of searching
    // every column the row displays.
    await expect(rowNamed(page, "bedrock-haiku")).toHaveCount(1);
    await expect(dataRows(page)).toHaveCount(1);
    await expect(page.getByTestId("models-filters-pill-search")).toContainText(
      "Search: haiku",
    );
  });

  await test.step("3. clear filters drops the term and every filter at once", async () => {
    await chooseFilter(page, "models-filters-filter-ns", "analytics");
    await expect(page.getByTestId("models-filters-pill-ns-analytics")).toBeVisible();

    await page.getByTestId("models-filters-pill-clear").click();

    // Everything: the term as well as the namespace. A term left in the box is the
    // filter a reader is most likely to have forgotten, so a control that cleared the
    // pills and left it would still be hiding rows.
    await expect(page.getByTestId("models-filters-pills")).toHaveCount(0);
    await expect(page.getByTestId("models-filters-search")).toHaveValue("");
    await expect(dataRows(page)).toHaveCount(4);
    await expect(page).toHaveURL(/\/models\?mock=ok$/);
  });

  await test.step("4. a term and a filter chosen in quick succession both survive", async () => {
    /*
     * A regression this build actually had. The two writes landed before React had
     * re-rendered, so the second read the address as it was before the first and put
     * the cleared filter straight back — a filter that would not clear, and a search
     * reporting no matches for a row plainly on the page. Driven without waiting in
     * between, because waiting is what hid it.
     */
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
