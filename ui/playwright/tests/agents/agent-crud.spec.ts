import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { agentNewConversation, dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";
import { anyDialog, confirmDelete, dialog, expectRequired, pressOnce, rowAction, selectOption } from "../../helpers/resource";
import { clickNav } from "../../helpers/nav";

type Source = "reference" | "inline";

const CASES: Source[] = ["reference", "inline"];

async function chooseTemplateSource(page: Page, source: Source) {
  await page.getByTestId("agent-form-template-source").getByText(source === "inline" ? "Define here" : "Use existing", { exact: true }).click();
}

for (const template of CASES) {
  const name = `crud-${template}`;
  const row = (page: Page) => rowNamed(page, name);

  test(`agents: CRUD with ${template === "inline" ? "an" : "a"} ${template} template and referenced harness`, async ({ page }) => {
    await test.step("1. create through the full-page form", async () => {
      await loadPage(page, routes.agents, { title: "Agents" });
      await page.getByTestId("agents-create").click();
      // The menu animates in, and a press on a moving item is dropped.
      await pressOnce(page.getByTestId("agents-new"));
      await expect(page).toHaveURL(/\/agents\/new$/);
      await expect(page.getByTestId("agent-form-namespace")).toContainText("kagent");
      await expect(page.getByTestId("form-back-link")).toHaveText("Agents");
      const submit = page.getByTestId("agent-form-submit");
      const error = (text: string) => page.locator(".ant-form-item-explain-error").getByText(text, { exact: true });

      // Create is held back beside a note; a field is flagged once it is left.
      await expect(submit).toHaveAttribute("aria-disabled", "true");
      await expect(page.getByTestId("agent-form-blocked")).toContainText("Fill in the required (*) fields.");
      await expect(error("A name is required.")).toHaveCount(0);
      await page.getByTestId("agent-form-name").focus();
      await page.getByTestId("agent-form-name").blur();
      await expect(error("A name is required.")).toBeVisible();
      await expect(page).toHaveURL(/\/agents\/new$/);
      await expect(page.getByTestId("agent-form-harness-ref")).toContainText("kagent-default");
      await expectRequired(page, {
        marked: ["Name", "Namespace", "Existing template", "Existing harness"],
        unmarked: ["Egress policies"],
      });
      await page.getByTestId("agent-form-name").fill(name);
      await expect(error("A name is required.")).toHaveCount(0);

      if (template === "inline") {
        await chooseTemplateSource(page, "inline");
        // Only a bring-your-own harness runs without a model, so the inline model is required.
        await expectRequired(page, { marked: ["Model configuration"], unmarked: [] });
        await expect(submit).toHaveAttribute("aria-disabled", "true");
        await selectOption(page, "template-form-model", "kagent/default-model-config");
        await page.getByTestId("template-form-description").fill("Inline before edit");
        await page.getByTestId("template-form-prompt").fill("You answer briefly.");
      } else {
        await selectOption(page, "agent-form-template-ref", "shared-brain");
      }
      await selectOption(page, "agent-form-harness-ref", "k8s-agent");
      await expect(page.getByTestId("agent-form-blocked")).toHaveCount(0);
      await submit.click();
    });

    await test.step("2. it appears in the list with its sources and status", async () => {
      await expect(page).toHaveURL(/\/agents\?tab=agents$/);
      await expect(row(page)).toHaveCount(1);
      const templateCell = page.getByTestId(`agent-template-kagent/${name}`);
      const harnessCell = page.getByTestId(`agent-harness-kagent/${name}`);
      await expect(templateCell).toHaveAttribute("data-source", template);
      await expect(harnessCell).toHaveAttribute("data-source", "reference");
      if (template === "reference") await expect(templateCell).toHaveText("shared-brain");
      await expect(harnessCell).toHaveText("k8s-agent");
      await expect(page.getByTestId(`agent-revision-kagent/${name}`)).toHaveText("Ready");
    });

    // One case is enough: the link does not depend on where the template comes from.
    if (template === "reference") {
      await test.step("3. opening it lands on its new conversation", async () => {
        await page.getByTestId(`agent-link-kagent-${name}`).click();
        await expect(page).toHaveURL(new RegExp(`${agentNewConversation({ name, template: "", harness: "" })}$`));
        await expect(page.getByTestId("new-conversation-empty")).toBeVisible();
        await expect(page.getByTestId("conversation-input")).toBeEditable();
      });
    }

    // In-app navigation from here on: the mock backend's writes live in page memory.
    await test.step("4. edit keeps the name fixed and saves the change", async () => {
      await clickNav(page, "agents", /\/agents$/);
      await rowAction(page, `kagent/${name}`, "edit");
      await expect(page).toHaveURL(new RegExp(`/agents/kagent/${name}/edit$`));
      await expect(page.getByTestId("agent-form-name")).toBeDisabled();
      await expect(page.getByTestId("agent-form-name")).toHaveValue(name);

      if (template === "inline") {
        await expect(page.getByTestId("template-form-description")).toHaveValue("Inline before edit");
        await page.getByTestId("template-form-description").fill("Inline after edit");
      } else {
        await expect(page.getByTestId("agent-form-template-ref")).toContainText("shared-brain");
        await selectOption(page, "agent-form-template-ref", "k8s-agent-7f3a91c");
      }
      await expect(page.getByTestId("agent-form-harness-ref")).toContainText("k8s-agent");
      await selectOption(page, "agent-form-harness-ref", "fast-lane");

      // A dirty form asks before Cancel leaves it; Keep editing stays with the edits.
      await page.getByTestId("agent-form-cancel").click();
      await expect(dialog(page)).toContainText("Discard your changes?");
      await pressOnce(dialog(page).getByRole("button", { name: "Keep editing" }));
      await expect(anyDialog(page)).toBeHidden();
      await expect(page).toHaveURL(new RegExp(`/agents/kagent/${name}/edit$`));

      await page.getByTestId("agent-form-submit").click();
      await expect(page).toHaveURL(/\/agents\?tab=agents$/);
    });

    await test.step("5. the same row shows the saved values, with no duplicate", async () => {
      await expect(row(page)).toHaveCount(1);
      if (template === "inline") await expect(row(page)).toContainText("Inline after edit");
      else await expect(page.getByTestId(`agent-template-kagent/${name}`)).toHaveText("k8s-agent-7f3a91c");
      await expect(page.getByTestId(`agent-harness-kagent/${name}`)).toHaveText("fast-lane");
    });

    await test.step("6. delete asks first, then the row is gone and the rest remain", async () => {
      await expectSettled(page);
      const before = await dataRows(page).count();
      await confirmDelete(page, `kagent/${name}`, { fromMenu: true });
      await expect(row(page)).toHaveCount(0);
      await expect(dataRows(page)).toHaveCount(before - 1);
    });
  });
}
