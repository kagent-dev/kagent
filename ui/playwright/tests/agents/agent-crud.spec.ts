import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { agentNewChat, dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";
import { confirmDelete, LIFECYCLE_TIMEOUT, pressOnce, selectOption } from "../../helpers/resource";
import { clickNav } from "../../helpers/nav";
import { operationAnswers, rpc } from "../../helpers/mockCalls";

type Source = "reference" | "inline";

const IMAGE = `ghcr.io/example/runtime@sha256:${"0".repeat(64)}`;

const CASES: { template: Source; harness: Source }[] = [
  { template: "reference", harness: "reference" },
  { template: "inline", harness: "reference" },
  { template: "reference", harness: "inline" },
  { template: "inline", harness: "inline" },
];

/*
 * A create, read, update and delete crosses six page loads and form fetches, so it gets
 * the lifecycle budget — see `LIFECYCLE_TIMEOUT`. On a loaded runner the longest case
 * spent the default thirty seconds with every step passing.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

/** A navigation's budget: a page mounting and reading, not one assertion's render. */
const NAVIGATION_TIMEOUT = 15_000;

/**
 * Starts counting the Agent list's reads; the returned wait resolves once a later
 * one has been answered.
 *
 * Create, save and delete each end with the list re-reading the Agents: on its mount
 * after the form navigates back, or by the delete's own refresh. Until that read is
 * answered the table shows what it had cached — no new Agent, the old values, the
 * deleted row. The URL changes the moment the form navigates, so a row assertion
 * after it had five seconds for the list's mount and its read, which a loaded runner
 * can spend on mounting alone. This waits on the read itself, with a navigation's
 * budget, and leaves the row assertion only the render.
 */
async function nextAgentListRead(page: Page): Promise<() => Promise<void>> {
  const before = await operationAnswers(page, rpc.listAgents);
  return async () => {
    await expect
      .poll(() => operationAnswers(page, rpc.listAgents), { timeout: NAVIGATION_TIMEOUT })
      .toBeGreaterThan(before);
  };
}

async function chooseSource(page: Page, kind: "template" | "harness", source: Source) {
  await page.getByTestId(`agent-form-${kind}-source`).getByText(source === "inline" ? "Inline" : "Reference", { exact: true }).click();
}

for (const { template, harness } of CASES) {
  const name = `crud-${template}-${harness}`;
  const row = (page: Page) => rowNamed(page, name);

  test(`agents: CRUD with a ${template} template and a ${harness} harness`, async ({ page }) => {
    let listed = async () => {};

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
      listed = await nextAgentListRead(page);
      await page.getByTestId("agent-form-submit").click();
    });

    await test.step("2. it appears in the list with its sources and status", async () => {
      await expect(page).toHaveURL(/\/agents\?tab=agents$/);
      await listed();
      await expect(row(page)).toHaveCount(1);
      const templateCell = page.getByTestId(`agent-template-kagent/${name}`);
      const harnessCell = page.getByTestId(`agent-harness-kagent/${name}`);
      await expect(templateCell).toHaveAttribute("data-source", template);
      await expect(harnessCell).toHaveAttribute("data-source", harness);
      if (template === "reference") await expect(templateCell).toHaveText("shared-brain");
      if (harness === "reference") await expect(harnessCell).toHaveText("k8s-agent");
      await expect(page.getByTestId(`agent-revision-kagent/${name}`)).toHaveText("Ready");
    });

    // One case is enough: the link does not depend on where the template or harness comes from.
    if (template === "reference" && harness === "reference") {
      await test.step("3. opening it lands on its new chat", async () => {
        await pressOnce(page.getByTestId(`agent-link-kagent-${name}`));
        await expect(page).toHaveURL(new RegExp(`${agentNewChat({ name, template: "", harness: "" })}$`));
        await expect(page.getByTestId("new-chat-empty")).toBeVisible();
        await expect(page.getByTestId("chat-input")).toBeEditable();
      });
    }

    // In-app navigation from here on: the mock backend's writes live in page memory.
    await test.step("4. edit keeps the name fixed and saves the change", async () => {
      await clickNav(page, "agents", /\/agents$/);
      // The URL changes before the list has mounted, so the row is waited for with a
      // navigation's budget; then held still, because the list's later reads re-lay its
      // rows and a click computed mid-shift lands beside the button.
      await expect(page.getByTestId(`edit-${name}`)).toBeVisible({ timeout: NAVIGATION_TIMEOUT });
      await pressOnce(page.getByTestId(`edit-${name}`));
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
      listed = await nextAgentListRead(page);
      await page.getByTestId("agent-form-submit").click();
      await expect(page).toHaveURL(/\/agents\?tab=agents$/);
    });

    await test.step("5. the same row shows the saved values, with no duplicate", async () => {
      await listed();
      await expect(row(page)).toHaveCount(1);
      if (template === "inline") await expect(row(page)).toContainText("Inline after edit");
      else await expect(page.getByTestId(`agent-template-kagent/${name}`)).toHaveText("k8s-agent-7f3a91c");
      if (harness === "reference") await expect(page.getByTestId(`agent-harness-kagent/${name}`)).toHaveText("fast-lane");

      // The inline harness is not on the row, so read it back from the form.
      if (harness === "inline") {
        await pressOnce(page.getByTestId(`edit-${name}`));
        await expect(page.getByTestId("harness-worker-pool")).toHaveValue("pool-after");
        await page.getByRole("button", { name: "Cancel", exact: true }).click();
        await expect(row(page)).toHaveCount(1);
      }
    });

    await test.step("6. delete asks first, then the row is gone and the rest remain", async () => {
      await expectSettled(page);
      const before = await dataRows(page).count();
      listed = await nextAgentListRead(page);
      await confirmDelete(page, name);
      await listed();
      await expect(row(page)).toHaveCount(0);
      await expect(dataRows(page)).toHaveCount(before - 1);
    });
  });
}
