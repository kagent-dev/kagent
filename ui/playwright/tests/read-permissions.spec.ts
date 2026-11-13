import { expect, test } from "../fixtures/test";
import { agentPage, agents, loadPage, routes } from "../helpers/app";
import { operationCalls, rpc } from "../helpers/mockCalls";
import { optionNamed } from "../helpers/resource";

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

test("refreshes the dashboard when unrelated reads are denied", async ({ page }) => {
  await loadPage(page, routes.dashboard, { scenario: "denied" });
  const alert = page.getByTestId("dashboard-access-denied");
  await expect(alert).toHaveText("You do not have access to namespaces and model configurations");
  await expect(alert.getByRole("button", { name: "Try again" })).toHaveCount(0);
  await expect(page.getByTestId("stat-agents-value")).toHaveText(/^[1-9]\d*$/);
  const reads = await operationCalls(page, rpc.listAgentInstances);
  await page.getByTestId("refresh-button").click();
  await expect(page.getByText("Dashboard refreshed")).toBeVisible();
  expect(await operationCalls(page, rpc.listAgentInstances)).toBeGreaterThan(reads);
  await expect(alert).toBeVisible();
});

test("offers readable agents in the switcher when namespace discovery is denied", async ({ page }) => {
  await loadPage(page, agentPage(agents.k8s), { scenario: "denied" });
  await page.getByTestId("agent-rail-identity").click();
  await expect(page.getByTestId(`agent-switcher-option-${agents.sharedOnK8s.name}`)).toBeVisible();
});

test("offers readable agents for a new schedule when namespace discovery is denied", async ({ page }) => {
  await loadPage(page, "/schedules/new", { scenario: "denied" });
  await page.getByTestId("schedule-agent").click();
  await expect(optionNamed(page, `kagent/${agents.k8s.name}`)).toBeVisible();
});
