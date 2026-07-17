import { test, expect } from "../../fixtures/test";
import { expectToast, waitForAppReady } from "../../helpers/page";

// Prompt libraries — one success journey + one failure journey (two videos), run
// against the real backend. /prompts redirects to ?namespace=kagent and lists via
// GET /api/prompttemplates?namespace=<ns>. Create is a dedicated route
// (/prompts/new) that POSTs then redirects to the detail page; edit PUTs, delete
// DELETEs.
//
// Success creates a uniquely-named throwaway library, opens its detail page, edits
// a fragment, then deletes it — only touching the library this test created.
//
// Failure keeps the client-side name-required validation.

const NAMESPACE = "kagent";

test("prompt library lifecycle: create, view, edit, and delete", async ({ page }, testInfo) => {
  const name = `e2e-prompts-${Date.now().toString(36)}-${testInfo.retry}`;

  await test.step("lists prompt libraries for the namespace", async () => {
    await page.goto("/prompts");
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { level: 1, name: "Prompt Libraries" })).toBeVisible();
  });

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

  await test.step("loads the library detail page", async () => {
    await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
    await expect(page.getByRole("button", { name: "Save changes" })).toBeVisible();
  });

  await test.step("edits a fragment and saves", async () => {
    await page.getByLabel("Key 1").fill("safety-rules");
    await page.getByLabel("Content").fill("Always be safe and kind.");
    await page.getByRole("button", { name: "Save changes" }).click();

    await expectToast(page, /saved/i, { type: "success" });
  });

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
  await page.goto(`/prompts/new?ns=${NAMESPACE}`);
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Create Library" }).click();

  await expectToast(page, /Library name is required/i, { type: "error" });
});
