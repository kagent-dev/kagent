import { test, expect } from "../fixtures/test";
import { loadPage, expectNoErrors } from "../helpers/page";
import { gotoSidebarLink, gotoCreatePath } from "../helpers/nav";

// App-shell journey — one test that walks the persistent shell end to end: the
// Agents list renders, then sidebar navigation to listing pages and create flows.
//
// The agents list shows the helm-seeded sample agents (k8s-agent, etc.) in the
// kagent namespace. We assert one of them is present rather than an exact count,
// so the test doesn't break as the seeded set evolves.
const SEEDED_AGENT = "k8s-agent";

test("app shell: list and navigation", async ({ page }) => {
  // region Reading — the agents list renders from the backend
  await test.step("renders the agents list from the real backend", async () => {
    const fatalErrors: string[] = [];
    page.on("pageerror", (err) => fatalErrors.push(err.message));

    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText(SEEDED_AGENT).first()).toBeVisible();
    await expectNoErrors(page);
    expect(fatalErrors, `uncaught page errors: ${fatalErrors.join("; ")}`).toEqual([]);
  });

  // region Navigating — reach listing pages via the sidebar
  await test.step("navigates between listing pages via the sidebar", async () => {
    await gotoSidebarLink(page, "Models", "**/models");
    await expect(page.getByRole("heading", { level: 1, name: "Models" })).toBeVisible();

    await gotoSidebarLink(page, "MCP & tools", "**/mcp");
    await expect(page.getByRole("heading", { level: 1, name: "MCP & tools" })).toBeVisible();

    await gotoSidebarLink(page, "Plugins Catalog", "**/plugins");
    await expect(page.getByRole("heading", { level: 1, name: "Plugins status" })).toBeVisible();
  });

  await test.step("navigates to create pages via listing actions and direct routes", async () => {
    await gotoCreatePath(page, "/agents/new", "**/agents/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Agent", exact: true })).toBeVisible();

    await gotoCreatePath(page, "/agents/new-harness", "**/agents/new-harness");
    await expect(page.getByRole("heading", { level: 1, name: "New Agent Harness" })).toBeVisible();

    await gotoSidebarLink(page, "Models", "**/models");
    await page.getByRole("button", { name: "New Model" }).click();
    await page.waitForURL("**/models/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Model" })).toBeVisible();

    await gotoSidebarLink(page, "MCP & tools", "**/mcp");
    await page.getByRole("link", { name: "Add server" }).click();
    await page.waitForURL("**/mcp/new");
    await expect(page.getByRole("heading", { level: 1, name: "New MCP server" })).toBeVisible();

    await gotoSidebarLink(page, "Prompt Library", "**/prompts");
    await page.getByRole("link", { name: "New Library" }).click();
    await page.waitForURL("**/prompts/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Prompt Library" })).toBeVisible();
  });
});
