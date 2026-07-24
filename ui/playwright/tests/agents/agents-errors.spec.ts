import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";

// Agents — error journey. Required-field validation on both create forms; these
// hold client-side, so no backend resources are created.

test("agents: create validation", async ({ page }) => {
  // region Creating — client-side validation blocks the POST
  await test.step("declarative create blocks submit + shows validation errors", async () => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByRole("button", { name: "Create Agent" }).click();

    // Scroll the first error in so it's on screen (in the recorded video).
    const descError = page.getByText("Description is required");
    await descError.scrollIntoViewIfNeeded();
    await expect(descError).toBeVisible();
    await expect(page.getByText("Please select a model")).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new/);
  });

  await test.step("harness create blocks submit when required fields are empty", async () => {
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page).toHaveURL(/\/agents\/new-harness/);
  });
});
