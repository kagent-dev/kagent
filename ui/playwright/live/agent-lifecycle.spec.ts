import { test, expect, type Page } from "@playwright/test";
import { expectNoLoadFailure, liveRoutes, loadLive, rowNamed } from "./helpers/live";
import { liveApi, type HarnessSpecShape } from "./helpers/api";

/**
 * Agent CRUD on a real cluster, through the UI, for each template/harness source.
 *
 * Needs the `assistant` Agent and AgentTemplate, the `kagent` Harness and
 * `default-model-config` in `kagent`. Inline harnesses copy that harness's image,
 * pool and snapshot store.
 */

const NAMESPACE = "kagent";
const TEMPLATE = "assistant";
const HARNESS = "kagent";
const MODEL = "default-model-config";
const RUN = Date.now().toString(36);
const READY_TIMEOUT = 180_000;

type Source = "reference" | "inline";
const CASES: { template: Source; harness: Source; slug: string }[] = [
  { template: "reference", harness: "reference", slug: "rr" },
  { template: "inline", harness: "reference", slug: "ir" },
  { template: "reference", harness: "inline", slug: "ri" },
  { template: "inline", harness: "inline", slug: "ii" },
];

const created = new Set<string>();
let harnessSpec: HarnessSpecShape;

test.beforeAll(async ({ baseURL }) => {
  harnessSpec = await liveApi(baseURL!).harnessSpec(NAMESPACE, HARNESS);
});

// Through the API, not the UI: teardown runs after failures, when the page may be anywhere.
test.afterAll(async ({ baseURL }) => {
  const api = liveApi(baseURL!);
  for (const name of created) await api.removeAgent(NAMESPACE, name);
});

async function pick(page: Page, testId: string, title: string) {
  await page.getByTestId(testId).click();
  await page.locator(`.ant-select-dropdown:visible .ant-select-item-option[title="${title}"]`).click();
}

async function chooseSource(page: Page, kind: "template" | "harness", source: Source) {
  await page
    .getByTestId(`agent-form-${kind}-source`)
    .getByText(source === "inline" ? "Inline" : "Reference", { exact: true })
    .click();
}

async function fillInlineTemplate(page: Page, description: string) {
  await chooseSource(page, "template", "inline");
  await pick(page, "template-form-model", MODEL);
  await page.getByTestId("template-form-description").fill(description);
  await page.getByTestId("template-form-prompt").fill("You are a concise assistant. Answer in one sentence.");
}

async function createAgent(page: Page, name: string, template: Source, harness: Source) {
  await loadLive(page, liveRoutes.agentNew);
  await expectNoLoadFailure(page);
  await page.getByTestId("agent-form-name").fill(name);
  if (template === "inline") await fillInlineTemplate(page, `Live ${name}`);
  else await pick(page, "agent-form-template-ref", TEMPLATE);
  if (harness === "inline") {
    await chooseSource(page, "harness", "inline");
    await page.getByTestId("harness-image").fill(harnessSpec.workload.image);
    await page.getByTestId("harness-worker-pool").fill(harnessSpec.substrate.workerPoolRef.name);
    await page.getByTestId("harness-snapshot").fill(`${harnessSpec.substrate.snapshotPolicy.location.replace(/\/$/, "")}/e2e-${name}`);
  } else {
    await pick(page, "agent-form-harness-ref", HARNESS);
  }
  created.add(name);
  await page.getByTestId("agent-form-submit").click();
  await expect(page).toHaveURL(/\/agents\?tab=agents$/, { timeout: 60_000 });
}

/** Re-reads the list until the controller has observed the latest edit and it succeeded. */
async function expectReady(page: Page, name: string) {
  const status = page.getByTestId(`agent-revision-${NAMESPACE}/${name}`);
  await expect
    .poll(
      async () => {
        await page.getByRole("button", { name: /Refresh/ }).click();
        return status.getAttribute("data-revision-state");
      },
      { timeout: READY_TIMEOUT, message: `${name} never became Ready` },
    )
    .toBe("ready");
}

for (const { template, harness, slug } of CASES) {
  const name = `e2e-${slug}-${RUN}`;

  test(`live: agent CRUD with a ${template} template and a ${harness} harness`, async ({ page, baseURL }) => {
    test.setTimeout(READY_TIMEOUT * 2 + 120_000);

    await test.step("1. create it through the form", async () => {
      await createAgent(page, name, template, harness);
    });

    await test.step("2. it is listed with its sources and becomes Ready", async () => {
      await expect(rowNamed(page, name)).toHaveCount(1, { timeout: 60_000 });
      await expect(page.getByTestId(`agent-template-${NAMESPACE}/${name}`)).toHaveAttribute("data-source", template);
      await expect(page.getByTestId(`agent-harness-${NAMESPACE}/${name}`)).toHaveAttribute("data-source", harness);
      await expectReady(page, name);
    });

    await test.step("3. opening it lands on its new chat", async () => {
      await page.getByTestId(`agent-link-${NAMESPACE}-${name}`).click();
      await expect(page).toHaveURL(new RegExp(`/agents/${NAMESPACE}/${name}/new$`));
      await expect(page.getByTestId("chat-input")).toBeEditable();
    });

    await test.step("4. an edit is saved, read back and becomes a new revision", async () => {
      const before = await liveApi(baseURL!).latestRevision(NAMESPACE, name);
      await loadLive(page, liveRoutes.agents);
      await page.getByTestId(`edit-${name}`).click();
      await expect(page.getByTestId("agent-form-name")).toBeDisabled();
      // A referenced template is switched to inline; an inline one gets a new description.
      await fillInlineTemplate(page, `Edited ${name}`);
      await page.getByTestId("agent-form-submit").click();
      await expect(page).toHaveURL(/\/agents\?tab=agents$/, { timeout: 60_000 });
      await expect(rowNamed(page, name)).toHaveCount(1);
      await expect(rowNamed(page, name)).toContainText(`Edited ${name}`);
      await expect(page.getByTestId(`agent-template-${NAMESPACE}/${name}`)).toHaveAttribute("data-source", "inline");
      await expectReady(page, name);
      expect(await liveApi(baseURL!).latestRevision(NAMESPACE, name)).not.toBe(before);
    });

    await test.step("5. delete asks first, then the row is gone and the rest remain", async () => {
      await page.getByTestId(`delete-${name}`).click();
      await page.locator(".ant-popconfirm:visible").getByRole("button", { name: "Delete" }).click();
      await expect(rowNamed(page, name)).toHaveCount(0, { timeout: 60_000 });
      // Other specs add and remove rows in parallel, so check a fixed neighbour, not a count.
      await expect(page.getByTestId(`agent-link-${NAMESPACE}-assistant`)).toBeVisible();
    });
  });
}

test("live: two agents with the same template and harness keep separate conversations", async ({ page }) => {
  test.setTimeout(READY_TIMEOUT * 2 + 240_000);
  const [first, second] = [`e2e-twin-a-${RUN}`, `e2e-twin-b-${RUN}`];
  let chatId = "";

  await test.step("1. create both from the same refs", async () => {
    await createAgent(page, first, "reference", "reference");
    await createAgent(page, second, "reference", "reference");
    await expectReady(page, first);
    await expectReady(page, second);
  });

  await test.step("2. chat with the first and get a real reply", async () => {
    await page.getByTestId(`agent-link-${NAMESPACE}-${first}`).click();
    await page.getByTestId("chat-input").fill("Say hello in three words.");
    await page.getByTestId("chat-send").click();
    await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/, { timeout: 60_000 });
    chatId = new URL(page.url()).pathname.split("/")[2];
    await expect(page.locator('[data-testid="chat-message"][data-role="agent"]')).toHaveCount(1, { timeout: 180_000 });
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0, { timeout: 180_000 });
    await expect(page.getByTestId(`chat-session-${chatId}`)).toBeVisible();
  });

  await test.step("3. the second agent does not list that chat", async () => {
    await loadLive(page, liveRoutes.agents);
    await page.getByTestId(`agent-link-${NAMESPACE}-${second}`).click();
    await expect(page.getByTestId("agent-rail-identity")).toContainText(second);
    await expect(page.getByTestId("chat-sessions-empty")).toBeVisible({ timeout: 60_000 });
    await expect(page.getByTestId(`chat-session-${chatId}`)).toHaveCount(0);
  });

  await test.step("4. and the first still does", async () => {
    await loadLive(page, liveRoutes.agents);
    await page.getByTestId(`agent-link-${NAMESPACE}-${first}`).click();
    await expect(page.getByTestId(`chat-session-${chatId}`)).toBeVisible({ timeout: 60_000 });
  });
});
