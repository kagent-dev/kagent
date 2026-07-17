import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";
import { selectOption, selectNamespace } from "../../helpers/select";
import { firstModelConfig } from "../../helpers/resources";

// Agents feature — a full-CRUD lifecycle journey plus a validation-failure journey
// (two videos). The lifecycle creates a uniquely-named declarative agent, reads it
// back on the edit page, updates its description, and deletes it — only ever
// touching the agent it created. The model config it attaches is discovered at
// runtime (firstModelConfig) rather than hard-coded, and an agent is only valid
// in the model config's namespace, so the agent is created there.
//
// Failure covers required-field validation on both create forms.

const DESCRIPTION = "e2e declarative agent";
const UPDATED_DESCRIPTION = "e2e declarative agent (edited)";

async function openEdit(page: import("@playwright/test").Page, ref: string) {
  await page.getByTestId(`agent-options-${ref}`).first().click();
  await page.getByRole("menuitem", { name: "Edit" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Edit Agent" })).toBeVisible();
}

test("agent lifecycle: create, read, update, delete", async ({ page, request }, testInfo) => {
  const { ref: modelRef, model, namespace } = await firstModelConfig(request);
  const modelOption = `${model} (${modelRef})`;
  const name = `e2e-agent-${Date.now().toString(36)}-${testInfo.retry}`;
  const ref = `${namespace}/${name}`;

  // region Creating — fill the form and POST a new declarative agent
  await test.step("creates a declarative agent", async () => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill(name);
    await page.getByLabel("Description").fill(DESCRIPTION);
    await selectNamespace(page, "#agent-field-namespace", namespace);
    await selectOption(page, "#agent-field-model", modelOption);

    await page.getByRole("button", { name: "Create Agent" }).click();
    await expect(page).toHaveURL(/\/agents(\?|$)/);
    await expect(page.getByText(name)).toBeVisible();
  });

  // region Reading — reopen the agent on its edit page and read the stored spec
  await test.step("reads the agent back on its edit page", async () => {
    await openEdit(page, ref);
    // The edit form loads the stored spec — proof the create persisted.
    await expect(page.getByLabel("Description")).toHaveValue(DESCRIPTION);
  });

  // region Updating — change the description, save (PUT), and confirm it persisted
  await test.step("updates the agent description", async () => {
    await page.getByLabel("Description").fill(UPDATED_DESCRIPTION);
    await page.getByRole("button", { name: "Save Changes" }).click();
    await expect(page).toHaveURL(/\/agents(\?|$)/);

    // Re-open to confirm the change persisted.
    await openEdit(page, ref);
    await expect(page.getByLabel("Description")).toHaveValue(UPDATED_DESCRIPTION);
    await loadPage(page, "/", { heading: "Agents" });
  });

  // region Deleting — remove the agent and confirm the card is gone
  await test.step("deletes the agent", async () => {
    await page.getByTestId(`agent-options-${ref}`).first().click();
    await page.getByRole("menuitem", { name: "Delete" }).click();
    const dialog = page.getByRole("alertdialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Delete" }).click();
    await expect(page.getByText(name)).toHaveCount(0);
  });
});

test("agent failures: validation blocks empty required fields", async ({ page }) => {
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
