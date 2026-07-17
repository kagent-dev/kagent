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

test("app shell: agents list and header navigation", async ({ page }) => {
  await test.step("renders the agents list from the real backend", async () => {
    const fatalErrors: string[] = [];
    page.on("pageerror", (err) => fatalErrors.push(err.message));

    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText(SEEDED_AGENT).first()).toBeVisible();
    await expectNoErrors(page);
    expect(fatalErrors, `uncaught page errors: ${fatalErrors.join("; ")}`).toEqual([]);
  });

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
    await expect(page.getByRole("heading", { level: 1, name: "New Agent", exact: true })).toBeVisible();

    await gotoCreate(page, "New Agent Harness", "**/agents/new-harness");
    await expect(page.getByRole("heading", { level: 1, name: "New Agent Harness" })).toBeVisible();

    await gotoCreate(page, "New Model", "**/models/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Model" })).toBeVisible();

    await gotoCreate(page, "New MCP Server", "**/mcp/new");
    await expect(page.getByRole("heading", { level: 1, name: "New MCP server" })).toBeVisible();

    await gotoCreate(page, "New prompt library", "**/prompts/new");
    await expect(page.getByRole("heading", { level: 1, name: "New Prompt Library" })).toBeVisible();
  });
});
