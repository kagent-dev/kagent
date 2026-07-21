import { test, expect } from "../fixtures/test";

// Stage 0 smoke test: proves the whole rig works end to end — Playwright boots
// the stub backend + `next dev`, the server-side fetch is redirected to the stub
// (via BACKEND_INTERNAL_URL), and the home page renders the mocked agent list.
test("home page renders the Agents list against the mocked backend", async ({ page }) => {
  const fatalErrors: string[] = [];
  page.on("pageerror", (err) => fatalErrors.push(err.message));

  await page.goto("/");

  // PageHeader renders <h1 id="agents-page-title">Agents</h1> on successful load
  // (present in both populated and empty states; absent in loading/error states).
  await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toBeVisible();

  // ErrorState (early-return branch) renders this heading — it must not appear.
  await expect(page.getByText("Error Encountered")).toHaveCount(0);

  expect(fatalErrors, `uncaught page errors: ${fatalErrors.join("; ")}`).toEqual([]);
});
