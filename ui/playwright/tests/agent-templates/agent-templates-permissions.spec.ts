import { test, expect } from "../../fixtures/test";
import { loadPage, routes } from "../../helpers/app";

/** Agent templates under a server that refuses actions. */
test("agent templates: refused create and delete are disabled", async ({ page }) => {
  await test.step("1. permitted, both are live", async () => {
    await loadPage(page, routes.agentTemplates, { title: "Agents" });
    await expect(page.getByTestId("agents-new-template")).toBeEnabled();
    await expect(page.locator('[data-testid^="delete-"]').first()).toBeEnabled();
  });

  await test.step("2. refused, both are disabled", async () => {
    await loadPage(page, routes.agentTemplates, { scenario: "denied", title: "Agents" });
    await expect(page.getByTestId("agents-new-template")).toBeDisabled();
    await expect(page.locator('[data-testid^="delete-"]').first()).toBeDisabled();
  });

  await test.step("3. the create names the permission the caller lacks", async () => {
    // The tooltip sits on a wrapper: a disabled antd button takes no pointer events.
    await page.getByTestId("agents-new-template").locator("..").hover();
    await expect(
      page.getByText("You do not have permission to create an agent template"),
    ).toBeVisible();
  });
});
