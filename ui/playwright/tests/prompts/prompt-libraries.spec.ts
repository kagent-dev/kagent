import { test, expect } from "../../fixtures/test";
import { expectToast, waitForAppReady } from "../../helpers/page";

// Prompt libraries — one success journey + one failure journey (two videos).
// /prompts redirects to ?namespace=kagent and lists via GET
// /api/prompttemplates?namespace=<ns>. Create is a dedicated route (/prompts/new)
// that POSTs /api/prompttemplates then redirects to the detail page; edit PUTs and
// delete DELETEs it.
//
// Success opens on the empty namespace, then walks the library lifecycle — create,
// view the detail page, edit fragments, delete+confirm. Failure is the single
// name-required validation block.

test("prompt library lifecycle: create, view, edit, and delete", async ({ page, mock }) => {
  await test.step("opens on the empty state for a namespace with no libraries", async () => {
    await page.goto("/prompts");
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { level: 1, name: "Prompt Libraries" })).toBeVisible();
    await expect(page.getByText("No prompt libraries in this namespace")).toBeVisible();
  });

  await test.step("creates a prompt library and POSTs the expected payload", async () => {
    await page.goto("/prompts/new?ns=kagent");
    await waitForAppReady(page);

    await page.getByLabel("Name", { exact: true }).fill("e2e-prompts");
    await page.getByLabel("Key 1").fill("safety-rules");
    await page.getByLabel("Content").fill("Always be safe.");
    await page.getByRole("button", { name: "Create Library" }).click();

    await expect(page).toHaveURL(/\/prompts\/kagent\/e2e-prompts/);
    await expectToast(page, /created/i, { type: "success" });

    const req = await mock.lastRequest<{ namespace: string; name: string; data: Record<string, string> }>(
      "POST",
      "/api/prompttemplates",
    );
    expect(req, "expected a captured POST /api/prompttemplates").not.toBeNull();
    expect(req!.body.namespace).toBe("kagent");
    expect(req!.body.name).toBe("e2e-prompts");
    expect(req!.body.data["safety-rules"]).toBe("Always be safe.");
  });

  await test.step("loads the library detail page", async () => {
    await page.goto("/prompts/kagent/e2e-prompts");
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { level: 1, name: "e2e-prompts" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Save changes" })).toBeVisible();
  });

  await test.step("edits fragments and PUTs the update", async () => {
    await page.getByLabel("Key 1").fill("safety-rules");
    await page.getByLabel("Content").fill("Always be safe.");
    await page.getByRole("button", { name: "Save changes" }).click();

    await expectToast(page, /saved/i, { type: "success" });
    const req = await mock.lastRequest<{ data: Record<string, string> }>(
      "PUT",
      "/api/prompttemplates/kagent/e2e-prompts",
    );
    expect(req).not.toBeNull();
    expect(req!.body.data["safety-rules"]).toBe("Always be safe.");
  });

  await test.step("deletes the library and confirms the DELETE", async () => {
    await page.getByRole("button", { name: "Delete", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete this prompt library?")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete library" }).click();

    await expectToast(page, /deleted/i, { type: "success" });
    await expect(page).toHaveURL(/\/prompts\?namespace=kagent/);
    expect(await mock.lastRequest("DELETE", "/api/prompttemplates/kagent/e2e-prompts")).not.toBeNull();
  });
});

test("prompt failures: blocks create when the name is empty", async ({ page, mock }) => {
  await page.goto("/prompts/new?ns=kagent");
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Create Library" }).click();

  await expectToast(page, /Library name is required/i, { type: "error" });
  expect((await mock.capturedRequests()).filter((r) => r.method === "POST")).toHaveLength(0);
});
