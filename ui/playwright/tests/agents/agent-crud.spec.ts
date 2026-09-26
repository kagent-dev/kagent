import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { agentNewChat, dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";
import { confirmDelete, selectOption } from "../../helpers/resource";
import { clickNav } from "../../helpers/nav";

type Source = "reference" | "inline";

const IMAGE = `ghcr.io/example/runtime@sha256:${"0".repeat(64)}`;

const CASES: { template: Source; harness: Source }[] = [
  { template: "reference", harness: "reference" },
  { template: "inline", harness: "reference" },
  { template: "reference", harness: "inline" },
  { template: "inline", harness: "inline" },
];

async function chooseSource(page: Page, kind: "template" | "harness", source: Source) {
  await page.getByTestId(`agent-form-${kind}-source`).getByText(source === "inline" ? "Inline" : "Reference", { exact: true }).click();
}

for (const { template, harness } of CASES) {
  const name = `crud-${template}-${harness}`;
  const row = (page: Page) => rowNamed(page, name);

  test(`agent CRUD: ${template} template, ${harness} harness`, async ({ page }) => {
    await test.step("1. create through the full-page form", async () => {
      await loadPage(page, routes.agents, { title: "Agents" });
      await page.getByTestId("agents-new").click();
      await expect(page).toHaveURL(/\/agents\/new$/);
      await expect(page.getByTestId("agent-form-namespace")).toContainText("kagent");
      await page.getByTestId("agent-form-name").fill(name);

      if (template === "inline") {
        await chooseSource(page, "template", "inline");
        await selectOption(page, "template-form-model", "default-model-config");
        await page.getByTestId("template-form-description").fill("Inline before edit");
        await page.getByTestId("template-form-prompt").fill("You answer briefly.");
      } else {
        await selectOption(page, "agent-form-template-ref", "shared-brain");
      }
      if (harness === "inline") {
        await chooseSource(page, "harness", "inline");
        await page.getByTestId("harness-image").fill(IMAGE);
        await page.getByTestId("harness-worker-pool").fill("pool-before");
        await page.getByTestId("harness-snapshot").fill("gs://snapshots/crud/");
      } else {
        await selectOption(page, "agent-form-harness-ref", "k8s-agent");
      }
      await page.getByTestId("agent-form-submit").click();
    });

    await test.step("2. it appears in the list with its sources and status", async () => {
      await expect(page).toHaveURL(/\/agents\?tab=agents$/);
      await expect(row(page)).toHaveCount(1);
      const templateCell = page.getByTestId(`agent-template-kagent/${name}`);
      const harnessCell = page.getByTestId(`agent-harness-kagent/${name}`);
      await expect(templateCell).toHaveAttribute("data-source", template);
      await expect(harnessCell).toHaveAttribute("data-source", harness);
      if (template === "reference") await expect(templateCell).toHaveText("shared-brain");
      if (harness === "reference") await expect(harnessCell).toHaveText("k8s-agent");
      await expect(page.getByTestId(`agent-revision-kagent/${name}`)).toHaveText("Ready");
    });

    await test.step("3. opening it lands on its new chat", async () => {
      await page.getByTestId(`agent-link-kagent-${name}`).click();
      await expect(page).toHaveURL(new RegExp(`${agentNewChat({ name, template: "", harness: "" })}$`));
      await expect(page.getByTestId("new-chat-empty")).toBeVisible();
      await expect(page.getByTestId("chat-input")).toBeEditable();
    });

    // In-app navigation from here on: the mock backend's writes live in page memory.
    await test.step("4. edit keeps the name fixed and saves the change", async () => {
      await clickNav(page, "agents", /\/agents$/);
      await page.getByTestId(`edit-${name}`).click();
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
      if (harness === "inline") {
        await expect(page.getByTestId("harness-worker-pool")).toHaveValue("pool-before");
        await page.getByTestId("harness-worker-pool").fill("pool-after");
      } else {
        await expect(page.getByTestId("agent-form-harness-ref")).toContainText("k8s-agent");
        await selectOption(page, "agent-form-harness-ref", "fast-lane");
      }
      await page.getByTestId("agent-form-submit").click();
      await expect(page).toHaveURL(/\/agents\?tab=agents$/);
    });

    await test.step("5. the same row shows the saved values, with no duplicate", async () => {
      await expect(row(page)).toHaveCount(1);
      if (template === "inline") await expect(row(page)).toContainText("Inline after edit");
      else await expect(page.getByTestId(`agent-template-kagent/${name}`)).toHaveText("k8s-agent-7f3a91c");
      if (harness === "reference") await expect(page.getByTestId(`agent-harness-kagent/${name}`)).toHaveText("fast-lane");

      // The inline harness is not on the row, so read it back from the form.
      if (harness === "inline") {
        await page.getByTestId(`edit-${name}`).click();
        await expect(page.getByTestId("harness-worker-pool")).toHaveValue("pool-after");
        await page.getByRole("button", { name: "Cancel", exact: true }).click();
        await expect(row(page)).toHaveCount(1);
      }
    });

    await test.step("6. delete asks first, then the row is gone and the rest remain", async () => {
      await expectSettled(page);
      const before = await dataRows(page).count();
      await confirmDelete(page, name);
      await expect(row(page)).toHaveCount(0);
      await expect(dataRows(page)).toHaveCount(before - 1);
    });
  });
}

test("agents with the same template and harness keep separate conversations", async ({ page }) => {
  const newChat = (name: string) => agentNewChat({ name, template: "", harness: "" });
  let chatId = "";

  await test.step("1. start a chat with one agent", async () => {
    await loadPage(page, newChat("shared-brain"));
    await page.getByTestId("chat-input").fill("Only for shared-brain");
    await page.getByTestId("chat-send").click();
    await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/);
    chatId = new URL(page.url()).pathname.split("/")[2];
    await expect(page.getByTestId(`chat-session-${chatId}`)).toBeVisible();
  });

  await test.step("2. its twin, with identical refs, does not list that chat", async () => {
    await clickNav(page, "agents", /\/agents$/);
    await page.getByTestId("agent-link-kagent-shared-brain-twin").click();
    await expect(page.getByTestId("agent-rail-identity")).toContainText("shared-brain-twin");
    await expect(page.getByTestId("chat-sessions-empty")).toBeVisible();
    await expect(page.getByTestId(`chat-session-${chatId}`)).toHaveCount(0);
  });

  await test.step("3. and the first agent still does", async () => {
    await clickNav(page, "agents", /\/agents$/);
    await page.getByTestId("agent-link-kagent-shared-brain").click();
    await expect(page.getByTestId(`chat-session-${chatId}`)).toBeVisible();
  });
});
