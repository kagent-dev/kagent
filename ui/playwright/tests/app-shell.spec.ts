import { test, expect } from "../fixtures/test";
import { loadPage, expectNoErrors } from "../helpers/page";
import { gotoView, gotoCreate } from "../helpers/nav";

// App-shell journey — one test that walks the persistent shell end to end:
// the Agents list in all three data states (populated / empty / error), then
// dropdown-based navigation to every listing and create page. Consolidates the
// former smoke + home + nav specs. State transitions re-navigate after changing
// the mock scenario (fetch is no-store, so a fresh goto picks up the override);
// mock.reset() restores the seeded happy path between phases.
test("app shell: agents list states and header navigation", async ({ page, mock }) => {
  await test.step("renders the agents list against the mocked backend", async () => {
    // Proves the whole rig works: Playwright boots the stub + `next dev`, the
    // server-side fetch is redirected to the stub, and the list renders.
    const fatalErrors: string[] = [];
    page.on("pageerror", (err) => fatalErrors.push(err.message));

    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText("e2e-agent")).toBeVisible();
    await expectNoErrors(page);
    expect(fatalErrors, `uncaught page errors: ${fatalErrors.join("; ")}`).toEqual([]);
  });

  await test.step("shows the empty state when there are no agents", async () => {
    await mock.noAgents();
    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByRole("heading", { level: 2, name: "No agents yet" })).toBeVisible();
  });

  await test.step("navigates between listing pages via the View menu", async () => {
    await mock.reset(); // back to the seeded happy path before client-side routing
    await loadPage(page, "/", { heading: "Agents" });

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

  // Last: the 500 path renders Next's global error boundary, which poisons the
  // dev server's client manifest for any subsequent full reload (a Turbopack dev
  // bug, not a product issue). Keeping it last means nothing navigates after it.
  await test.step("shows the error state when the agents request fails", async () => {
    await mock.agentsError();
    await page.goto("/");
    await expect(page.getByText("Error Encountered")).toBeVisible();
    // ErrorState early-returns, so the page <h1> never renders.
    await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toHaveCount(0);
  });
});
