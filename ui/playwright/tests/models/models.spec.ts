import { test, expect } from "../../fixtures/test";
import { loadPage, expectToast, waitForAppReady } from "../../helpers/page";

// Models / providers — one success journey + one failure journey (two videos).
// The provider auto-selects OpenAI; the Model dropdown reads /api/models (keyed
// "OpenAI"). Create POSTs /api/modelconfigs; edit PUTs /api/modelconfigs/<ns>/<name>;
// delete DELETEs it. All captured by the stub for payload assertions.
//
// Success walks the config lifecycle — create (with and without an API key),
// edit-save, delete+confirm. Failure consolidates the create-validation block
// and the delete-failure toast into one video.

const MODEL_NAME = "gpt-4o";

test("model lifecycle: create, edit, and delete a config", async ({ page, mock }) => {
  await test.step("creates a model config and POSTs the expected payload", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    await page.getByTestId("model-provider-select").click();
    await page.getByRole("option", { name: "OpenAI" }).click();
    await page.getByTestId("model-select").click();
    await page.getByRole("option", { name: MODEL_NAME }).click();
    await page.getByTestId("model-api-key-input").fill("sk-e2e-test-key");

    await page.getByRole("button", { name: "Create Model" }).click();

    await expect(page).toHaveURL(/\/models(\?|$)/);
    await expectToast(page, /created successfully/i, { type: "success" });

    const req = await mock.lastRequest<{ apiKey?: string; spec: { model: string; provider: string } }>(
      "POST",
      "/api/modelconfigs",
    );
    expect(req, "expected a captured POST /api/modelconfigs").not.toBeNull();
    expect(req!.body.spec.model).toBe(MODEL_NAME);
    expect(req!.body.spec.provider).toBe("OpenAI");
    expect(req!.body.apiKey).toBe("sk-e2e-test-key");
  });

  await test.step("creates a model without an API key via the gateway checkbox", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    await page.getByTestId("model-provider-select").click();
    await page.getByRole("option", { name: "OpenAI" }).click();
    await page.getByTestId("model-select").click();
    await page.getByRole("option", { name: MODEL_NAME }).click();
    await page.getByRole("checkbox", { name: "I don't need to provide an API key" }).check();

    await page.getByRole("button", { name: "Create Model" }).click();

    await expect(page).toHaveURL(/\/models(\?|$)/);
    const req = await mock.lastRequest<{ apiKey?: string; spec: { model: string } }>("POST", "/api/modelconfigs");
    expect(req).not.toBeNull();
    expect(req!.body.spec.model).toBe(MODEL_NAME);
    expect(req!.body.apiKey).toBeUndefined();
  });

  await test.step("edits an existing model config and PUTs the update", async () => {
    await loadPage(page, "/models", { heading: "Models" });

    // Open the seeded model's edit page (data-test hook the Cypress test relies on).
    await page.locator('[data-test^="edit-model-"]').first().click();
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { level: 1, name: "Edit Model" })).toBeVisible();

    // All fields are prefilled; saving requires no edits.
    await page.getByRole("button", { name: "Save Changes" }).click();

    await expect(page).toHaveURL(/\/models(\?|$)/);
    await expectToast(page, /updated successfully/i, { type: "success" });

    const req = await mock.lastRequest("PUT", "/api/modelconfigs/default/default-model-config");
    expect(req, "expected a captured PUT /api/modelconfigs/<ref>").not.toBeNull();
  });

  await test.step("deletes a model config and confirms the DELETE", async () => {
    await loadPage(page, "/models", { heading: "Models" });

    await page.getByRole("button", { name: "Delete model default/default-model-config" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete Model")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete" }).click();

    await expectToast(page, /deleted successfully/i, { type: "success" });
    expect(
      await mock.lastRequest("DELETE", "/api/modelconfigs/default/default-model-config"),
      "expected a captured DELETE",
    ).not.toBeNull();
  });
});

test("model failures: create validation and delete error", async ({ page, mock }) => {
  await test.step("blocks create when no model is selected", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    await page.getByRole("button", { name: "Create Model" }).click();

    await expect(page.getByText("Provider and Model selection is required")).toBeVisible();
    await expect(page).toHaveURL(/\/models\/new/);
    expect((await mock.capturedRequests()).filter((r) => r.method === "POST")).toHaveLength(0);
  });

  await test.step("shows an error toast when model delete fails", async () => {
    await mock.reset();
    await mock.setMutation("DELETE", "/api/modelconfigs/default/default-model-config", {
      status: 500,
      body: { error: "boom" },
    });
    await loadPage(page, "/models", { heading: "Models" });

    await page.getByRole("button", { name: "Delete model default/default-model-config" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Delete" }).click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
  });
});
