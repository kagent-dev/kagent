import { test, expect } from "../fixtures/test";
import { loadPage, expectNoErrors } from "../helpers/page";
import { gotoView, gotoCreate } from "../helpers/nav";

// App-shell journey — one test that walks the persistent shell end to end: the
// Agents list renders, then dropdown-based navigation to every listing and create
// page.
//
// The agents list shows the helm-seeded sample agents (k8s-agent, etc.) in the
// kagent namespace. We assert one of them is present rather than an exact count,
// so the test doesn't break as the seeded set evolves.
const SEEDED_AGENT = "k8s-agent";

// The create pages are each compiled on-demand the first time they're hit (the CI
// run uses the webpack dev server, not a prod build), so their first navigation can
// exceed the default 10s expect timeout on a loaded runner. Give the create-page
// heading assertions extra headroom to absorb that one-time compile latency.
const COLD_NAV_TIMEOUT = 30_000;

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

  // region Navigating — reach every listing and create page via the header menus
  await test.step("navigates between listing pages via the View menu", async () => {
    await gotoView(page, "Models", "**/models");
    await expect(page.getByRole("heading", { level: 1, name: "Models" })).toBeVisible();

    await gotoView(page, "MCP & tools", "**/mcp");
    await expect(page.getByRole("heading", { level: 1, name: "MCP & tools" })).toBeVisible();
  });

  await test.step("navigates to create pages via the Create menu", async () => {
    // The Create menu lives in the persistent header, so we navigate client-side
    // from wherever the View step left us (no extra full reload of "/").
    await gotoCreate(page, "New Agent", "**/agents/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Agent", exact: true })).toBeVisible({ timeout: COLD_NAV_TIMEOUT });

    await gotoCreate(page, "New Agent Harness", "**/agents/new-harness");
    await expect(page.getByRole("heading", { level: 1, name: "New Agent Harness" })).toBeVisible({ timeout: COLD_NAV_TIMEOUT });

    await gotoCreate(page, "New Model", "**/models/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Model" })).toBeVisible({ timeout: COLD_NAV_TIMEOUT });

    await gotoCreate(page, "New MCP Server", "**/mcp/new");
    await expect(page.getByRole("heading", { level: 1, name: "New MCP server" })).toBeVisible({ timeout: COLD_NAV_TIMEOUT });

    await gotoCreate(page, "New prompt library", "**/prompts/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Prompt Library" })).toBeVisible({ timeout: COLD_NAV_TIMEOUT });
  });
});
