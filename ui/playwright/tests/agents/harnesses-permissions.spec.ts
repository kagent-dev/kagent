import { test, expect } from "../../fixtures/test";
import { loadPage, routes } from "../../helpers/app";

/**
 * Harnesses under a server that refuses actions.
 *
 * A Harness has no update RPC, so it carries only `canDelete` — there is no edit
 * control here to gate.
 */
test("harnesses: refused create and delete are disabled", async ({ page }) => {
  await test.step("1. permitted, both are live", async () => {
    await loadPage(page, routes.harnesses, { title: "Agents" });
    await expect(page.getByTestId("harnesses-table")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId("agents-new-harness")).toBeEnabled();
    await expect(page.locator('[data-testid^="delete-"]').first()).toBeEnabled();
  });

  await test.step("2. refused, both are disabled", async () => {
    await loadPage(page, routes.harnesses, { scenario: "denied", title: "Agents" });
    await expect(page.getByTestId("harnesses-table")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId("agents-new-harness")).toBeDisabled();
    await expect(page.locator('[data-testid^="delete-"]').first()).toBeDisabled();
  });

  await test.step("3. the create names the permission the caller lacks", async () => {
    // The tooltip sits on a wrapper: a disabled antd button takes no pointer events.
    await page.getByTestId("agents-new-harness").locator("..").hover();
    await expect(
      page.getByText("You do not have permission to create a harness"),
    ).toBeVisible();
  });
});
