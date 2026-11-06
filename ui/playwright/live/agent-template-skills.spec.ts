import { test, expect, type Page } from "@playwright/test";
import { createClient } from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-web";
import { AgentTemplateService } from "../../src/generated/kagent/api/v1/agent_templates_pb";
import { expectNoLoadFailure, liveRoutes, loadLive, throwawayName } from "./helpers/live";
import { liveApi } from "./helpers/api";
import { pressOnce } from "../helpers/resource";

/**
 * Skills in the agent template form, against a real controller.
 *
 * Needs the chart's built-in skills catalog in `KAGENT_UI_BUILTIN_SKILLS` (the value the
 * chart writes to the UI ConfigMap), the `kagent` Harness and `default-model-config` in
 * `kagent`. The chat test also needs a working model behind `default-model-config`.
 */

const NAMESPACE = "kagent";
const HARNESS = "kagent";
const MODEL = `${NAMESPACE}/default-model-config`;
const BUILTIN = "k8s-troubleshooting";
const READY_TIMEOUT = 180_000;

// A real public repository and commit. A template does not fetch its skills, so these
// only have to be well formed; an Agent would need them to exist.
const GIT_URL = "https://github.com/kagent-dev/kagent.git";
const GIT_COMMIT = "8cc31cfe7b9ea951bf3d21478e5a9b00f55025f6";
const S3 = {
  endpoint: "https://s3.amazonaws.com",
  bucket: "e2e-skills",
  key: "skills.tar.gz",
  versionId: "3HL4kqtJlcpXroDTDmJ.rmSpXd3dIbrHY",
};

const RAW_CATALOG = process.env.KAGENT_UI_BUILTIN_SKILLS ?? "";
const catalog = RAW_CATALOG
  ? (JSON.parse(RAW_CATALOG) as { image: string; skills: { name: string; path: string }[] })
  : undefined;

const templates = new Set<string>();
const agents = new Set<string>();

test.skip(!catalog, "KAGENT_UI_BUILTIN_SKILLS is not set to the chart's skills catalog");

// Through the API, not the UI: teardown runs after failures, when the page may be anywhere.
test.afterAll(async ({ baseURL }) => {
  const api = liveApi(baseURL!);
  for (const name of agents) await api.removeAgent(NAMESPACE, name);
  const client = templateClient(baseURL!);
  for (const name of templates) {
    await client.deleteAgentTemplate({ ref: { namespace: NAMESPACE, name } }).catch(() => {});
  }
});

function templateClient(baseURL: string) {
  return createClient(AgentTemplateService, createGrpcWebTransport({ baseUrl: `${baseURL}/api` }));
}

/** The template's spec as the controller stores it. */
async function storedSpec(baseURL: string, name: string) {
  const { agentTemplate } = await templateClient(baseURL).getAgentTemplate({ ref: { namespace: NAMESPACE, name } });
  return (agentTemplate?.resource?.value as { spec?: { description?: string; skills?: unknown[] } } | undefined)?.spec;
}

/**
 * Picks an option from the dropdown just opened. Each skill row's kind select has its own
 * dropdown with the same titles, and the last one can still be fading out; antd appends a
 * dropdown to the body the first time it opens, so the newest open one is the last.
 */
async function choose(page: Page, testId: string, title: string) {
  await page.getByTestId(testId).click();
  const open = page.locator(".ant-select-dropdown:not(.ant-select-dropdown-hidden)").last();
  await pressOnce(open.locator(`.ant-select-item-option[title="${title}"]`));
}

async function addBuiltinSkill(page: Page, name: string) {
  await choose(page, "template-form-add-builtin-skill", name);
}

async function addOwnSkill(page: Page, index: number, name: string, kind: "OCI image" | "Git" | "S3") {
  await page.getByTestId("template-form-add-skill").click();
  await page.getByTestId(`template-form-skill-name-${index}`).fill(name);
  if (kind !== "OCI image") await choose(page, `template-form-skill-kind-${index}`, kind);
}

test("live: agent template skills are saved, read back and kept through an edit", async ({ page, baseURL }) => {
  test.setTimeout(240_000);
  const name = throwawayName("skills");
  const helmPath = catalog!.skills.find((skill) => skill.name === "helm-release-management")!.path;
  const builtinPath = catalog!.skills.find((skill) => skill.name === BUILTIN)!.path;
  const expectedSkills = [
    { name: BUILTIN, source: { oci: catalog!.image, path: builtinPath } },
    { name: "own-helm", source: { oci: catalog!.image, path: helmPath } },
    { name: "own-git", source: { git: { url: GIT_URL, commit: GIT_COMMIT }, path: "skills/k8s-troubleshooting" } },
    { name: "own-s3", source: { bucket: { s3: S3 }, path: "skills/incident-review" } },
  ];

  await test.step("1. the page carries the chart's catalog", async () => {
    await loadLive(page, "/agent-templates/new");
    await expectNoLoadFailure(page);
    const served = await page.evaluate(
      () => (window as { environmentVariables?: Record<string, string> }).environmentVariables?.BUILTIN_SKILLS,
    );
    expect(served).toBe(RAW_CATALOG);
  });

  await test.step("2. create a template with a kagent skill and an OCI, a Git and an S3 skill", async () => {
    await page.getByTestId("template-form-name").fill(name);
    await choose(page, "template-form-model", MODEL);
    await page.getByTestId("template-form-description").fill(`Live ${name}`);
    await page.getByTestId("template-form-prompt").fill("You are a concise assistant.");

    await addBuiltinSkill(page, BUILTIN);
    await expect(page.getByTestId("template-form-skill-0")).toContainText("kagent");
    await expect(page.getByTestId("template-form-skill-0")).toContainText(BUILTIN);

    await addOwnSkill(page, 1, "own-helm", "OCI image");
    await page.getByTestId("template-form-skill-oci-1").fill(catalog!.image);
    await page.getByTestId("template-form-skill-path-1").fill(helmPath);

    await addOwnSkill(page, 2, "own-git", "Git");
    await page.getByTestId("template-form-skill-git-url-2").fill(GIT_URL);
    await page.getByTestId("template-form-skill-git-commit-2").fill(GIT_COMMIT);
    await page.getByTestId("template-form-skill-path-2").fill("skills/k8s-troubleshooting");

    await addOwnSkill(page, 3, "own-s3", "S3");
    await page.getByTestId("template-form-skill-s3-endpoint-3").fill(S3.endpoint);
    await page.getByTestId("template-form-skill-s3-bucket-3").fill(S3.bucket);
    await page.getByTestId("template-form-skill-s3-key-3").fill(S3.key);
    await page.getByTestId("template-form-skill-s3-version-3").fill(S3.versionId);
    await page.getByTestId("template-form-skill-path-3").fill("skills/incident-review");

    await expect(page.getByTestId("template-form-problems")).toHaveCount(0);
    templates.add(name);
    await page.getByTestId("template-submit").click();
    await expect(page).toHaveURL(/\/agents\?tab=templates/, { timeout: 60_000 });
  });

  await test.step("3. the controller stored exactly those four skills", async () => {
    expect((await storedSpec(baseURL!, name))?.skills).toEqual(expectedSkills);
  });

  await test.step("4. reading it back shows the kagent skill as one and the others' sources", async () => {
    await loadLive(page, `/agent-templates/${NAMESPACE}/${name}`);
    await expectNoLoadFailure(page);
    await expect(page.getByTestId("template-edit")).toBeVisible({ timeout: 60_000 });
    await expect(page.getByTestId("template-form-add-skill")).toHaveCount(0);

    const builtin = page.getByTestId("template-form-skill-0");
    await expect(builtin.locator(".ant-tag")).toHaveText("kagent");
    await expect(builtin).toContainText(BUILTIN);

    await expect(page.getByTestId("template-form-skill-name-1")).toHaveValue("own-helm");
    await expect(page.getByTestId("template-form-skill-oci-1")).toHaveValue(catalog!.image);
    await expect(page.getByTestId("template-form-skill-path-1")).toHaveValue(helmPath);

    await expect(page.getByTestId("template-form-skill-name-2")).toHaveValue("own-git");
    await expect(page.getByTestId("template-form-skill-kind-2")).toContainText("Git");
    await expect(page.getByTestId("template-form-skill-git-url-2")).toHaveValue(GIT_URL);
    await expect(page.getByTestId("template-form-skill-git-commit-2")).toHaveValue(GIT_COMMIT);

    await expect(page.getByTestId("template-form-skill-name-3")).toHaveValue("own-s3");
    await expect(page.getByTestId("template-form-skill-kind-3")).toContainText("S3");
    await expect(page.getByTestId("template-form-skill-s3-endpoint-3")).toHaveValue(S3.endpoint);
    await expect(page.getByTestId("template-form-skill-s3-bucket-3")).toHaveValue(S3.bucket);
    await expect(page.getByTestId("template-form-skill-s3-key-3")).toHaveValue(S3.key);
    await expect(page.getByTestId("template-form-skill-s3-version-3")).toHaveValue(S3.versionId);
  });

  await test.step("5. an edit to the description keeps the skills", async () => {
    await page.getByTestId("template-edit").click();
    await page.getByTestId("template-form-description").fill(`Edited ${name}`);
    await page.getByTestId("template-submit").click();
    await expect(page.getByTestId("template-edit")).toBeVisible({ timeout: 60_000 });
    const spec = await storedSpec(baseURL!, name);
    expect(spec?.description).toBe(`Edited ${name}`);
    expect(spec?.skills).toEqual(expectedSkills);
  });

  await test.step("6. a short Git commit and a name with a slash are refused before saving", async () => {
    await loadLive(page, "/agent-templates/new");
    await page.getByTestId("template-form-name").fill(throwawayName("refused"));
    await choose(page, "template-form-model", MODEL);
    await page.getByTestId("template-form-prompt").fill("You are a concise assistant.");
    await addOwnSkill(page, 0, "team/runbooks", "Git");
    await page.getByTestId("template-form-skill-git-url-0").fill(GIT_URL);
    await page.getByTestId("template-form-skill-git-commit-0").fill("abc");

    const problems = page.getByTestId("template-form-problems");
    await expect(problems).toContainText('The skill "team/runbooks" needs a name that is one path segment');
    await expect(problems).toContainText('The skill "team/runbooks" needs a full Git commit ID: 40 or 64 hex characters.');
    await expect(page.getByTestId("template-submit")).toBeDisabled();
  });
});

test("live: an agent built with a kagent skill loads it in chat", async ({ page }, testInfo) => {
  test.setTimeout(READY_TIMEOUT + 300_000);
  const name = throwawayName("skill-chat");

  await test.step("1. create an Agent whose inline template has only the kagent skill", async () => {
    await loadLive(page, liveRoutes.agentNew);
    await expectNoLoadFailure(page);
    await page.getByTestId("agent-form-name").fill(name);
    await page.getByTestId("agent-form-template-source").getByText("Inline", { exact: true }).click();
    await choose(page, "template-form-model", MODEL);
    await page.getByTestId("template-form-description").fill(`Live ${name}`);
    await page
      .getByTestId("template-form-prompt")
      .fill(
        "You are a Kubernetes assistant. Before you answer, find the skill whose description matches the task and load it with your skill tool. When you cite a skill, put its text in quotes.",
      );
    await addBuiltinSkill(page, BUILTIN);
    await expect(page.getByTestId("template-form-skill-0")).toContainText(BUILTIN);
    await choose(page, "agent-form-harness-ref", HARNESS);
    agents.add(name);
    await page.getByTestId("agent-form-submit").click();
    await expect(page).toHaveURL(/\/agents\?tab=agents$/, { timeout: 60_000 });
  });

  await test.step("2. the Agent becomes Ready, so the kagent-tools image was pulled", async () => {
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
  });

  await test.step("3. asked about a crashing pod, it loads the skill", async () => {
    await page.getByTestId(`agent-link-${NAMESPACE}-${name}`).click();
    await page
      .getByTestId("chat-input")
      .fill(
        // Not "word for word": Azure OpenAI's content filter reads that as a jailbreak and refuses the request.
        "A pod in namespace default is in CrashLoopBackOff. Which of your skills applies? Load it, then tell me the opening line of its instructions in quotes.",
      );
    await page.getByTestId("chat-send").click();
    await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}\/chat$/, { timeout: 60_000 });
    // The turn has started once the agent says something, and has ended once it can no longer be cancelled.
    await expect(page.locator('[data-testid="chat-message"][data-role="agent"]').first()).toBeVisible({ timeout: 240_000 });
    await expect(page.getByTestId("chat-cancel")).toHaveCount(0, { timeout: 240_000 });

    const call = page.locator('[data-testid="chat-tool-call"][data-tool-name="load_skill"]');
    await expect(call.first()).toBeVisible();
    await expect(call.first().getByTestId("chat-tool-payload")).toContainText(BUILTIN);
    // The skill's own text, from opt/kagent-tools-skills/skills/k8s-troubleshooting/SKILL.md in the kagent-tools image.
    const loaded = page.locator('[data-testid="chat-tool-result"][data-tool-name="load_skill"]');
    await expect(loaded.first().getByTestId("chat-tool-payload")).toContainText("Work from symptoms to cause.");
    // The reply's wording varies between runs (it sometimes quotes the heading), so the
    // skill's text is checked in the tool result above, not in the reply.
    await expect(page.locator('[data-testid="chat-message"][data-role="agent"]').last()).not.toBeEmpty();
    await page.screenshot({ path: testInfo.outputPath("chat-reply.png"), fullPage: true });
    testInfo.annotations.push({ type: "session", description: new URL(page.url()).pathname.split("/")[2] });
  });
});

