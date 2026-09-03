import { test, expect } from "../../fixtures/test";
import {
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
  withScenario,
} from "../../helpers/app";
import { operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * Models — the reading journey, and the way in to the create form.
 *
 * As with agents, the old spec was a create → read → update → delete lifecycle. The
 * create half runs against a real cluster instead (`live/write/models-create.spec.ts`),
 * because a form that only ever posts to a fixture proves the fixture. What is covered
 * here is the list, and that the page offers a way to reach the form — the header's
 * create menu belongs to the default shell, so a distribution supplying its own layout
 * does not inherit it, and this button is then the only way in.
 */

const CONFIGS = [
  "default-model-config",
  "anthropic-model-config",
  "ollama-local",
  "bedrock-haiku",
];

test("models: the list loads and renders each configuration", async ({ page }) => {
  await test.step("1. a loading state precedes the data", async () => {
    await page.goto(withScenario(routes.models, "slow"));
    await expect(page.locator(".ant-spin-spinning")).toBeVisible();
  });

  await test.step("2. every configuration is listed", async () => {
    for (const name of CONFIGS) {
      await expect(rowNamed(page, name), `"${name}" is missing`).toHaveCount(1);
    }
    await expect(dataRows(page)).toHaveCount(CONFIGS.length);
    await expectSettled(page);
  });

  await test.step("3. each row splits the ref and shows its provider", async () => {
    // The API returns one `namespace/name` string; the list has to take it apart
    // to fill two columns, which is the part worth pinning.
    const openai = rowNamed(page, "default-model-config");
    await expect(openai).toContainText("kagent");
    await expect(openai).toContainText("OpenAI");
    await expect(openai).toContainText("gpt-4.1");

    const ollama = rowNamed(page, "ollama-local");
    await expect(ollama).toContainText("platform");
    await expect(ollama).toContainText("Ollama");
    // No API key secret on a local provider — the column shows a dash, not blank.
    await expect(ollama).toContainText("—");
  });

  await test.step("4. refreshing re-reads the list without disturbing it", async () => {
    // Operations, not requests: under the substituted transport a working refresh
    // makes no HTTP request for `page.on("request")` to see.
    const before = await operationCalls(page, rpc.listModelConfigs);

    await page.getByRole("button", { name: "Refresh" }).click();
    await expect
      .poll(() => operationCalls(page, rpc.listModelConfigs), { timeout: 10_000 })
      .toBeGreaterThan(before);

    await expectSettled(page);
    await expect(dataRows(page)).toHaveCount(CONFIGS.length);
  });

  await test.step("5. an empty result says so instead of showing a bare table", async () => {
    await loadPage(page, routes.models, { scenario: "empty", title: "Models" });
    await expect(page.getByText("No model configurations yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("6. the list offers a way to create one", async () => {
    // From the empty list, which is where somebody most needs it.
    await page.getByTestId("models-new").click();
    await page.waitForURL(/\/models\/new(\?|$)/);
    // A field of the real form, not just the route: `ModelForm` is the same component the
    // edit page uses, so reaching its provider picker is reaching the thing that can
    // actually create a model.
    await expect(page.getByTestId("model-provider")).toBeVisible();
  });
});
