import { test, expect } from "../fixtures/test";
import { loadPage, routes, withScenario } from "../helpers/app";

/**
 * What Refresh says when it works, and when it does not.
 *
 * The second half is the reason this spec exists. A refresh usually returns the same
 * data, so a successful one is indistinguishable from a button that did nothing —
 * hence the confirmation. But the first version of that confirmation reported success
 * unconditionally, because SWR captures a failed revalidation into its error state
 * and resolves anyway. The toast then sat above a page showing a load error, saying
 * the opposite of it, and only a real click revealed that.
 */


/**
 * Clicks Refresh once it is actually clickable.
 *
 * The control carries the list's own loading state, and antd ignores a click on a
 * button that is loading — so clicking too early refreshes nothing and the missing
 * toast looks like a missing feature. Waiting for spinners to clear is not enough:
 * straight after a navigation there are no spinners yet, so that check passes before
 * the page has even mounted.
 */
async function clickRefresh(page: import("@playwright/test").Page): Promise<void> {
  const button = page.getByTestId("refresh-button");
  await expect(button).toBeEnabled();
  await expect(button).not.toHaveClass(/ant-btn-loading/);
  await button.click();
}

test("refresh: the confirmation says what actually happened", async ({ page }) => {
  /* "Agents", not "Agent page": the control lives in this tab's own filter row and
     re-reads this tab, so it names what it refreshed. It briefly refreshed all three
     from the page header, and moving it beside the filters is what made naming one
     correct again. */
  await test.step("1. a refresh that worked says so", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await clickRefresh(page);

    await expect(page.getByText("Agents refreshed")).toBeVisible();
  });

  await test.step("2. a refresh that failed says that instead", async () => {
    // The scenario has to be asked for explicitly: it persists across navigation, so
    // a page loaded without it would inherit whatever the last one used.
    //
    // Narrowed to one namespace, which is what makes the *agent* read happen at all:
    // with no namespace chosen the page fans out over the namespace list, and in this
    // scenario that list is the read that fails first — so the page would be reporting
    // a namespace failure rather than the refresh failure under test.
    //
    // `ns` is the filter's own parameter, the one `useListView` reads. An older
    // `?scope=&namespace=` pair selects nothing now, which is a silent no-op: the page
    // still loads, still shows an error, and the step passes its first assertion while
    // testing something other than what it says.
    await page.goto(withScenario(`${routes.agents}?ns=kagent`, "error"));
    await expect(page.getByTestId("agents-error")).toBeVisible();

    await clickRefresh(page);

    // Names the resource and carries the reason, and — the point — does not claim a
    // refresh that did not happen.
    await expect(page.getByText(/Could not refresh Agents/)).toBeVisible();
    await expect(page.getByText("Agents refreshed")).toHaveCount(0);
  });
});

test("refresh: every list confirms, not just the first one wired up", async ({
  page,
}) => {
  // One assertion per list, because the toast is the kind of thing that gets added
  // to the page being worked on and forgotten on the four beside it.
  for (const [path, message] of [
    [routes.models, "Models refreshed"],
    [routes.mcpServers, "Tool servers refreshed"],
    [routes.prompts, "Prompt libraries refreshed"],
    [routes.dashboard, "Dashboard refreshed"],
  ] as const) {
    await page.goto(withScenario(path, "ok"));
    await clickRefresh(page);
    await expect(page.getByText(message)).toBeVisible();
  }
});
