import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";
import { selectOption } from "../../helpers/select";

// Sub-stage 2.5 — the harness/BYO create variant at /agents/new-harness. Minimal
// harness = name + model (namespace defaults to "default", backend to openclaw).
// Submit POSTs /api/agentharnesses (AgentHarness CR), captured by the stub.

const MODEL_OPTION = "gpt-4o (default/default-model-config)";

test.describe("create agent (harness)", () => {
  test("creates a harness agent and POSTs an AgentHarness CR", async ({ page, mock }) => {
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByLabel("Agent name").fill("e2e-harness");
    await selectOption(page, "#agent-field-model", MODEL_OPTION);

    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page).toHaveURL(/\/agents(\?|$)/);

    const req = await mock.lastRequest<{
      kind: string;
      metadata: { name: string };
      spec: { backend: string; modelConfigRef: string };
    }>("POST", "/api/agentharnesses");
    expect(req, "expected a captured POST /api/agentharnesses").not.toBeNull();
    expect(req!.body.kind).toBe("AgentHarness");
    expect(req!.body.metadata.name).toBe("e2e-harness");
    expect(req!.body.spec.backend).toBe("openclaw");
  });

  test("blocks submit when required fields are empty", async ({ page, mock }) => {
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page).toHaveURL(/\/agents\/new-harness/);
    expect(await mock.capturedRequests()).toHaveLength(0);
  });

  test("creates a Hermes-backed harness", async ({ page, mock }) => {
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByLabel("Agent name").fill("e2e-hermes");
    await selectOption(page, "#agent-field-model", MODEL_OPTION);
    await selectOption(page, "#agent-harness-field-type", "Hermes");
    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page).toHaveURL(/\/agents(\?|$)/);
    const req = await mock.lastRequest<{ spec: { backend: string } }>("POST", "/api/agentharnesses");
    expect(req).not.toBeNull();
    expect(req!.body.spec.backend).toBe("hermes");
  });

  test("shows an error toast when harness create fails", async ({ page, mock }) => {
    await mock.setMutation("POST", "/api/agentharnesses", { status: 500, body: { error: "boom" } });
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByLabel("Agent name").fill("e2e-fail-harness");
    await selectOption(page, "#agent-field-model", MODEL_OPTION);
    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new-harness/);
  });
});
