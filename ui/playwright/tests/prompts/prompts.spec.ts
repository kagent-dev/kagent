import { test, expect } from "../../fixtures/test";
import { loadPage, expectToast, waitForAppReady } from "../../helpers/page";

// Prompt libraries — full-CRUD lifecycle journey. /prompts lists via GET
// /api/prompttemplates?namespace=<ns>; create is a dedicated route that POSTs then
// redirects to the detail page; edit PUTs, delete DELETEs. Each mutation is
// verified back on the list page: create adds the row, an edit that adds a fragment
// bumps the row's key count, and delete removes the row. Only the library this test
// creates is touched. Error journeys live in prompts-errors.spec.ts.

const NAMESPACE = "kagent";

// The list renders each library as a link whose text includes the name and "<n> keys".
function libraryRow(page: import("@playwright/test").Page, name: string) {
  return page.getByRole("link", { name: new RegExp(name) });
}

test("prompts: create, read, update, delete", async ({ page }, testInfo) => {
  const name = `e2e-prompts-${Date.now().toString(36)}-${testInfo.retry}`;

  // region Creating — POST a new library, then confirm the row on the prompts list
  await test.step("creates a library and sees it on the list", async () => {
    await page.goto(`/prompts/new?ns=${NAMESPACE}`);
    await waitForAppReady(page);

    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByLabel("Key 1").fill("safety-rules");
    await page.getByLabel("Content").fill("Always be safe.");
    await page.getByRole("button", { name: "Create Library" }).click();

    await expect(page).toHaveURL(new RegExp(`/prompts/${NAMESPACE}/${name}`));
    await expectToast(page, /created/i, { type: "success" });

    await loadPage(page, "/prompts", { heading: "Prompt Libraries" });
    await expect(libraryRow(page, name)).toContainText("1 keys");
  });

  // region Reading — open the library's detail page from the list
  await test.step("opens the library detail page", async () => {
    await libraryRow(page, name).click();
    await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
    await expect(page.getByRole("button", { name: "Save changes" })).toBeVisible();
  });

  // region Updating — add a fragment, save (PUT), then confirm the key count on the list
  await test.step("adds a fragment and sees the updated count on the list", async () => {
    await page.getByRole("button", { name: "Add fragment" }).click();
    await page.getByLabel("Key 2").fill("tone");
    await page.getByLabel("Content").nth(1).fill("Be kind.");
    await page.getByRole("button", { name: "Save changes" }).click();
    await expectToast(page, /saved/i, { type: "success" });

    await loadPage(page, "/prompts", { heading: "Prompt Libraries" });
    await expect(libraryRow(page, name)).toContainText("2 keys");
  });

  // region Deleting — delete from the detail page, then confirm the row is gone
  await test.step("deletes the library and sees it removed from the list", async () => {
    await libraryRow(page, name).click();
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete this prompt library?")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete library" }).click();

    await expectToast(page, /deleted/i, { type: "success" });
    await expect(page).toHaveURL(new RegExp(`/prompts\\?namespace=${NAMESPACE}`));
    await expect(libraryRow(page, name)).toHaveCount(0);
  });
});
