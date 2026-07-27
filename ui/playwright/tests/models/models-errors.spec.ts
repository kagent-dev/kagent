import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";

// Models — error journey. Client-side model-selection validation on create; holds
// without a backend round trip.

test("models: create validation", async ({ page }) => {
  // region Creating — client-side validation blocks the POST
  await test.step("blocks create when no model is selected", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    await page.getByRole("button", { name: "Create Model" }).click();

    // The error renders up by the model field; scroll it in so it's on screen
    // (in the recorded video) rather than above the fold.
    const error = page.getByText("Provider and Model selection is required");
    await error.scrollIntoViewIfNeeded();
    await expect(error).toBeVisible();
    await expect(page).toHaveURL(/\/models\/new/);
  });
});
