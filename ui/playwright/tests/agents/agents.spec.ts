import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";
import { selectOption, selectNamespace } from "../../helpers/select";

// Agents feature — one success journey + one failure journey (two videos), run
// against the real backend.
//
// Success creates uniquely-named throwaway agents (declarative + harness),
// verifies they appear in the list, then deletes them — only ever touching
// resources this test created, never the helm-seeded ones. Names carry a per-run
// suffix so re-runs never collide and a crashed run leaves no duplicates.
//
// Failure covers required-field validation on both create forms.

// The seeded model config lives in the "kagent" namespace, and a model is only
// selectable when its namespace matches the agent's — so we create agents there.
const NAMESPACE = "kagent";
// Model dropdown label = `${spec.model} (${ref})` for the seeded model config.
const MODEL_OPTION = "gpt-4.1-mini (kagent/default-model-config)";

async function deleteAgent(page: import("@playwright/test").Page, ref: string) {
  await page.getByTestId(`agent-options-${ref}`).first().click();
  await page.getByRole("menuitem", { name: "Delete" }).click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Delete" }).click();
  await expect(dialog).toHaveCount(0);
}

test("agent lifecycle: create (declarative + harness) then delete", async ({ page }, testInfo) => {
  // Generated per attempt (Date.now differs on retry) so re-runs never collide.
  const run = `${Date.now().toString(36)}-${testInfo.retry}`;
  const DECL_AGENT = `e2e-decl-${run}`;
  const HARNESS_AGENT = `e2e-harness-${run}`;

  await test.step("creates a declarative agent", async () => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill(DECL_AGENT);
    await page.getByLabel("Description").fill("e2e declarative agent");
    await selectNamespace(page, "#agent-field-namespace", NAMESPACE);
    await selectOption(page, "#agent-field-model", MODEL_OPTION);

    await page.getByRole("button", { name: "Create Agent" }).click();
    await expect(page).toHaveURL(/\/agents(\?|$)/);
  });

  await test.step("creates a harness (BYO) agent", async () => {
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByLabel("Agent name").fill(HARNESS_AGENT);
    await selectNamespace(page, "#agent-harness-field-namespace", NAMESPACE);
    await selectOption(page, "#agent-field-model", MODEL_OPTION);

    await page.getByRole("button", { name: "Create harness" }).click();
    await expect(page).toHaveURL(/\/agents(\?|$)/);
  });

  await test.step("shows both new agents in the list", async () => {
    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText(DECL_AGENT)).toBeVisible();
    await expect(page.getByText(HARNESS_AGENT)).toBeVisible();
  });

  await test.step("deletes both agents it created", async () => {
    await deleteAgent(page, `${NAMESPACE}/${DECL_AGENT}`);
    await expect(page.getByText(DECL_AGENT)).toHaveCount(0);

    await deleteAgent(page, `${NAMESPACE}/${HARNESS_AGENT}`);
    await expect(page.getByText(HARNESS_AGENT)).toHaveCount(0);
  });
});

test("agent failures: validation blocks empty required fields", async ({ page }) => {
  await test.step("declarative create blocks submit + shows validation errors", async () => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByRole("button", { name: "Create Agent" }).click();

    await expect(page.getByText("Description is required")).toBeVisible();
    await expect(page.getByText("Please select a model")).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new/);
  });

  await test.step("harness create blocks submit when required fields are empty", async () => {
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page).toHaveURL(/\/agents\/new-harness/);
  });
});
