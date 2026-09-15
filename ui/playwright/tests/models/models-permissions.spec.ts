import { test, expect } from "../../fixtures/test";
import { loadPage, rowNamed, routes } from "../../helpers/app";

/**
 * Model configurations under a server that refuses actions.
 *
 * Each case asserts the permitted state too: a regression that disables every
 * control passes a test that only looks at the refused one.
 */

const CONFIG = "default-model-config";

test("models: a refused create is disabled and says why", async ({ page }) => {
  await test.step("1. permitted, the create is a live link", async () => {
    await loadPage(page, routes.models, { title: "Models" });
    await expect(page.getByTestId("models-new")).toBeEnabled();
  });

  await test.step("2. refused, it is disabled", async () => {
    await loadPage(page, routes.models, { scenario: "denied", title: "Models" });
    await expect(page.getByTestId("models-new")).toBeDisabled();
  });

  await test.step("3. and names the permission the caller lacks", async () => {
    // The tooltip sits on a wrapper: a disabled antd button takes no pointer events.
    await page.getByTestId("models-new").locator("..").hover();
    await expect(
      page.getByText("You do not have permission to create a model configuration"),
    ).toBeVisible();
  });
});

test("models: refused row actions are disabled", async ({ page }) => {
  await loadPage(page, routes.models, { title: "Models" });
  const permitted = rowNamed(page, CONFIG);
  await expect(permitted.getByTestId(`edit-${CONFIG}`)).toBeEnabled();
  await expect(permitted.getByTestId(`delete-${CONFIG}`)).toBeEnabled();

  await loadPage(page, routes.models, { scenario: "denied", title: "Models" });
  const refused = rowNamed(page, CONFIG);
  await expect(refused.getByTestId(`edit-${CONFIG}`)).toBeDisabled();
  await expect(refused.getByTestId(`delete-${CONFIG}`)).toBeDisabled();
});

test("models: the create page explains itself rather than offering a form", async ({
  page,
}) => {
  // Reachable by URL, where there is no control to disable.
  await loadPage(page, routes.modelNew, { scenario: "denied" });

  await expect(page.getByText("You cannot create model configurations")).toBeVisible();
  await expect(page.getByRole("button", { name: /create/i })).toHaveCount(0);
});
