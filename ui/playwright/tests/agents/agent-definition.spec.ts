import { test, expect } from "../../fixtures/test";
import { loadPage, routes } from "../../helpers/app";
import { optionNamed } from "../../helpers/resource";

for (const template of ["reference", "inline"]) {
  for (const harness of ["reference", "inline"]) {
    test(`Agent definition: ${template} template and ${harness} Harness`, async ({ page }) => {
      await loadPage(page, routes.agents);
      await page.getByRole("button", {name:"New Agent", exact:true}).click();
      const modal = page.getByRole("dialog");
      await modal.locator("#agent-namespace").click();
      await optionNamed(page, "kagent").click();
      const name = `agent-${template}-${harness}`;
      await modal.getByLabel("Name", {exact:true}).fill(name);
      if (template === "inline") {
        await modal.locator('[aria-label="Template source"]').getByText("Inline", {exact:true}).click();
        await modal.locator("#agent-template").fill(JSON.stringify({systemPrompt:"Inline instructions", modelConfig:{name:"default-model-config"}}));
      } else {
        await modal.locator("#agent-template").fill("shared-brain");
      }
      if (harness === "inline") {
        await modal.locator('[aria-label="Harness source"]').getByText("Inline", {exact:true}).click();
        await modal.locator("#agent-harness").fill(JSON.stringify({kagent:{}, workload:{image:`example.com/runtime@sha256:${"0".repeat(64)}`}, substrate:{workerPoolRef:{name:"kagent-default"},snapshotPolicy:{location:"s3://snapshots"}}}));
      } else {
        await modal.locator("#agent-harness").fill("k8s-agent");
      }
      await modal.getByRole("button", {name:"Create Agent", exact:true}).click();
      await expect(page).toHaveURL(new RegExp(`/agents/kagent/${name}$`));
      await expect(page.getByTestId("agent-cannot-start")).toBeVisible();
      await page.getByRole("button", {name:"Edit Agent", exact:true}).click();
      await expect(modal.locator("#agent-template")).toHaveValue(template === "reference" ? "shared-brain" : /Inline instructions/);
      await expect(modal.locator("#agent-harness")).toHaveValue(harness === "reference" ? "k8s-agent" : /workload/);
      const updatedTemplate = template === "reference"
        ? "k8s-agent-7f3a91c"
        : JSON.stringify({systemPrompt:"Updated instructions", modelConfig:{name:"default-model-config"}});
      await modal.locator("#agent-template").fill(updatedTemplate);
      await modal.getByRole("button", {name:"Save Agent", exact:true}).click();
      await expect(modal).not.toBeVisible();
      await page.getByRole("button", {name:"Edit Agent", exact:true}).click();
      await expect(modal.locator("#agent-template")).toHaveValue(template === "reference" ? updatedTemplate : /Updated instructions/);
      await expect(modal.locator("#agent-harness")).toHaveValue(harness === "reference" ? "k8s-agent" : /workload/);
      await modal.getByRole("button", {name:"Cancel", exact:true}).click();
    });
  }
}

test("Agent descriptions resolve from shared templates and remain searchable", async ({ page }) => {
  await loadPage(page, routes.agents);
  await expect(page.getByText("One configuration, run on two different runtimes.", {exact: true})).toHaveCount(2);
  await page.getByPlaceholder("Search agents").fill("One configuration");
  await expect(page.getByRole("link", {name: "shared-brain", exact: true})).toBeVisible();
  await expect(page.getByRole("link", {name: "shared-brain-fast", exact: true})).toBeVisible();
  await expect(page.getByRole("link", {name: "support-triage-2b91d0e", exact: true})).toHaveCount(0);
});
