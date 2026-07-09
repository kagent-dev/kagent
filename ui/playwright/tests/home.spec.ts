import { test, expect } from "../fixtures/test";
import { loadPage, expectNoErrors } from "../helpers/page";

// Exercises the scenario-override engine (via the `mock` fixture) across the
// three states of the Agents list. Also the first real consumer of page.ts.
test.describe("home / agents list", () => {
  test("renders the agent (happy path)", async ({ page }) => {
    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText("e2e-agent")).toBeVisible();
    await expectNoErrors(page);
  });

  test("shows the empty state when there are no agents", async ({ page, mock }) => {
    await mock.noAgents();
    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByRole("heading", { level: 2, name: "No agents yet" })).toBeVisible();
  });

  test("shows the error state when the agents request fails", async ({ page, mock }) => {
    await mock.agentsError();
    await page.goto("/");
    await expect(page.getByText("Error Encountered")).toBeVisible();
    // ErrorState early-returns, so the page <h1> never renders.
    await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toHaveCount(0);
  });
});
