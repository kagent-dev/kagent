import { test, expect } from "../../fixtures/test";
import { loadPage, expectToast } from "../../helpers/page";

// Models / providers — one success journey + one failure journey (two videos),
// run against the real backend.
//
// Success creates a uniquely-named throwaway model config (OpenAI + a real
// catalog model + dummy key), edits it, then deletes it — never touching the
// seeded default-model-config that agents depend on. The form's namespace
// combobox auto-selects "kagent", so the config is kagent/<name>. Per-item edit
// and delete controls are scoped by the config ref (aria-label), so we only ever
// act on the config this test created.
//
// Failure covers the client-side model-selection validation.

const NAMESPACE = "kagent";
// A model that exists in the real OpenAI catalog served by /api/models.
const MODEL_NAME = "gpt-5.4-mini";

test("model lifecycle: create, edit, and delete a config", async ({ page }, testInfo) => {
  const name = `e2e-model-${Date.now().toString(36)}-${testInfo.retry}`;
  const ref = `${NAMESPACE}/${name}`;

  await test.step("creates a model config", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    // Provider + model are searchable cmdk comboboxes; type to filter, then click.
    // Option accessible names include icon alt text (e.g. "OpenAI icon OpenAI"), so
    // we anchor the provider match to avoid matching "AzureOpenAI" and pick the
    // first filtered model option.
    await page.getByTestId("model-provider-select").click();
    await page.getByPlaceholder("Search providers...").fill("OpenAI");
    await page.getByRole("option", { name: /^OpenAI\b/ }).first().click();

    await page.getByTestId("model-select").click();
    await page.getByPlaceholder("Search models...").fill(MODEL_NAME);
    await page.getByRole("option").first().click();

    // Override the auto-generated name so it's unique and scoped to this run.
    await page.locator('[data-test="edit-model-name-button"]').click();
    await page.getByPlaceholder("Enter model name...").fill(name);

    await page.getByTestId("model-api-key-input").fill("sk-e2e-test-key");
    await page.getByRole("button", { name: "Create Model" }).click();

    await expect(page).toHaveURL(/\/models(\?|$)/);
    await expectToast(page, /created successfully/i, { type: "success" });
    await expect(page.getByRole("button", { name: `Edit model ${ref}` })).toBeVisible();
  });

  await test.step("edits the config it created", async () => {
    await page.getByRole("button", { name: `Edit model ${ref}` }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Edit Model" })).toBeVisible();

    // All fields are prefilled; saving requires no edits.
    await page.getByRole("button", { name: "Save Changes" }).click();

    await expect(page).toHaveURL(/\/models(\?|$)/);
    await expectToast(page, /updated successfully/i, { type: "success" });
  });

  await test.step("deletes the config it created", async () => {
    await page.getByRole("button", { name: `Delete model ${ref}` }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete Model")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete" }).click();

    await expectToast(page, /deleted successfully/i, { type: "success" });
    await expect(page.getByRole("button", { name: `Delete model ${ref}` })).toHaveCount(0);
  });
});

test("model failures: create validation", async ({ page }) => {
  await test.step("blocks create when no model is selected", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    await page.getByRole("button", { name: "Create Model" }).click();

    await expect(page.getByText("Provider and Model selection is required")).toBeVisible();
    await expect(page).toHaveURL(/\/models\/new/);
  });
});
