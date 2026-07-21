import { test, expect } from "../../fixtures/test";
import { loadPage, expectScrolledIntoView } from "../../helpers/page";

// Models / providers — full-CRUD lifecycle journey. Creates a uniquely-named
// throwaway config (OpenAI + a real catalog model + dummy key), reads it back on
// the edit page, updates it, then deletes it — never touching a seeded config.
// Per-item edit/delete controls are scoped by the config ref, so only the config
// this test created is acted on. Error journeys live in models-errors.spec.ts.

const NAMESPACE = "kagent";
// A model that exists in the real OpenAI catalog served by /api/models.
const MODEL_NAME = "gpt-5.4-mini";

test("models: create, read, update, delete", async ({ page }, testInfo) => {
  const name = `e2e-model-${Date.now().toString(36)}-${testInfo.retry}`;
  const ref = `${NAMESPACE}/${name}`;

  // region Creating — fill the form and POST a new model config
  await test.step("creates a model config", async () => {
    await loadPage(page, "/models/new", { heading: "New Model" });

    // Provider + model are searchable cmdk comboboxes; type to filter, then click.
    // Option accessible names include icon alt text (e.g. "OpenAI icon OpenAI"), so
    // anchor the provider match to avoid "AzureOpenAI" and take the first model hit.
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

    // Verify the create on the actual models list: the new config's row is present
    // (scrolled into view).
    await expect(page).toHaveURL(/\/models(\?|$)/);
    await expectScrolledIntoView(page.getByRole("button", { name: `Edit model ${ref}` }));
  });

  // region Reading — open the edit page and load the stored config
  await test.step("reads the config back on its edit page", async () => {
    await page.getByRole("button", { name: `Edit model ${ref}` }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Edit Model" })).toBeVisible();
  });

  // region Updating — rotate the API key and save (PUT)
  await test.step("updates the config's API key", async () => {
    // In edit mode only the API key is editable (provider/model/name are locked).
    // The key is write-only, so it can't be read back; a successful PUT is confirmed
    // by the redirect to the list with the config still present.
    await page.getByTestId("model-api-key-input").fill("sk-e2e-rotated-key");
    await page.getByRole("button", { name: "Save Changes" }).click();
    // The rotated API key is write-only and never rendered on the list, so a model
    // update produces no list-visible change. The list-level check is that the
    // config's row survives the save (scrolled into view); a failed PUT keeps you on
    // the edit page.
    await expect(page).toHaveURL(/\/models(\?|$)/);
    await expectScrolledIntoView(page.getByRole("button", { name: `Edit model ${ref}` }));
  });

  // region Deleting — remove the config and confirm the row is gone
  await test.step("deletes the config", async () => {
    await page.getByRole("button", { name: `Delete model ${ref}` }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete Model")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete" }).click();

    // The config's row disappearing from the list is the durable delete signal.
    await expect(page.getByRole("button", { name: `Delete model ${ref}` })).toHaveCount(0);
  });
});
