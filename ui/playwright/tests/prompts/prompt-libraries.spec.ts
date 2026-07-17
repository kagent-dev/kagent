import { test, expect } from "../../fixtures/test";
import { expectToast, waitForAppReady } from "../../helpers/page";

// Prompt libraries — a full-CRUD lifecycle journey plus a validation-failure
// journey (two videos). /prompts lists via GET /api/prompttemplates?namespace=<ns>;
// create is a dedicated route that POSTs then redirects to the detail page; edit
// PUTs, delete DELETEs. The lifecycle creates a uniquely-named library, reads its
// detail page, edits a fragment, then deletes it — only touching what it created.

const NAMESPACE = "kagent";

test("prompt library lifecycle: create, read, update, delete", async ({ page }, testInfo) => {
  const name = `e2e-prompts-${Date.now().toString(36)}-${testInfo.retry}`;

  // region Creating — fill the form and POST a new prompt library
  await test.step("creates a prompt library", async () => {
    await page.goto(`/prompts/new?ns=${NAMESPACE}`);
    await waitForAppReady(page);

    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByLabel("Key 1").fill("safety-rules");
    await page.getByLabel("Content").fill("Always be safe.");
    await page.getByRole("button", { name: "Create Library" }).click();

    await expect(page).toHaveURL(new RegExp(`/prompts/${NAMESPACE}/${name}`));
    await expectToast(page, /created/i, { type: "success" });
  });

  // region Reading — load the library's detail page
  await test.step("reads the library detail page", async () => {
    await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
    await expect(page.getByRole("button", { name: "Save changes" })).toBeVisible();
  });

  // region Updating — edit a fragment and save (PUT)
  await test.step("edits a fragment and saves", async () => {
    await page.getByLabel("Key 1").fill("safety-rules");
    await page.getByLabel("Content").fill("Always be safe and kind.");
    await page.getByRole("button", { name: "Save changes" }).click();

    await expectToast(page, /saved/i, { type: "success" });
  });

  // region Deleting — remove the library and confirm the redirect
  await test.step("deletes the library and confirms", async () => {
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete this prompt library?")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete library" }).click();

    await expectToast(page, /deleted/i, { type: "success" });
    await expect(page).toHaveURL(new RegExp(`/prompts\\?namespace=${NAMESPACE}`));
  });
});

test("prompt failures: blocks create when the name is empty", async ({ page }) => {
  // region Creating — client-side validation blocks the POST
  await page.goto(`/prompts/new?ns=${NAMESPACE}`);
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Create Library" }).click();

  await expectToast(page, /Library name is required/i, { type: "error" });
});
