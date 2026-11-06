import { expect, test } from "../fixtures/test";
import { loadPage, routes } from "../helpers/app";

for (const [route, table] of [
  [routes.agents, "agents-table"],
  [routes.agentTemplates, "templates-table"],
  [routes.harnesses, "harnesses-table"],
]) {
  test(`lists resources on ${route} when namespace discovery is denied`, async ({ page }) => {
    await loadPage(page, route, { scenario: "denied" });
    await expect(page.getByTestId(table).locator("tbody tr.ant-table-row").first()).toBeVisible();
  });
}

test("shows permission denial without a retry or an empty list message", async ({ page }) => {
  await loadPage(page, routes.models, { scenario: "denied" });
  const alert = page.getByTestId("models-error");
  await expect(alert).toHaveText("You do not have access to model configurations");
  await expect(alert.getByRole("button", { name: "Try again" })).toHaveCount(0);
  await expect(page.getByText("No model configurations yet.")).toHaveCount(0);
});

test("keeps the dashboard agent count when unrelated reads are denied", async ({ page }) => {
  await loadPage(page, routes.dashboard, { scenario: "denied" });
  const alert = page.getByTestId("dashboard-access-denied");
  await expect(alert).toHaveText("You do not have access to namespaces and model configurations");
  await expect(alert.getByRole("button", { name: "Try again" })).toHaveCount(0);
  await expect(page.getByTestId("stat-agents-value")).toHaveText(/^[1-9]\d*$/);
});
