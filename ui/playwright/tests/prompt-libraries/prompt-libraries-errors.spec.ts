import { test } from "../../fixtures/test";
import { expectToast, waitForAppReady } from "../../helpers/page";

const NAMESPACE = "kagent";

// Prompt libraries — error journey. Client-side name-required validation on create.

test("prompt libraries: name validation", async ({ page }) => {
  // region Creating — client-side validation blocks the POST
  await page.goto(`/prompts/new?ns=${NAMESPACE}`);
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Create Library" }).click();

  await expectToast(page, /Library name is required/i, { type: "error" });
});
