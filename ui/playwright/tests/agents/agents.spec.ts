import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";
import { selectOption } from "../../helpers/select";

// Agents feature — one success journey + one failure journey (two videos).
//
// Success walks the whole agent lifecycle as steps: create a fully-configured
// declarative agent, create both harness backends (BYO), then delete an agent and
// confirm the DELETE. Data calls are server-side (stub); create/delete are captured
// server-side for payload assertions. The stub is stateless, so each phase
// re-navigates fresh and asserts on the captured request rather than on list state.
//
// Failure consolidates every negative/edge path into one video: validation blocks
// (declarative + harness), error toasts (declarative + harness), and delete-cancel.
// Each step calls mock.reset() first so it starts from a clean slate — captured
// requests and scenario overrides don't leak between steps.

// Model option label = `${spec.model} (${ref})` for the seeded modelconfig.
const MODEL_OPTION = "gpt-4o (default/default-model-config)";

async function openDeleteDialog(page: import("@playwright/test").Page) {
  await page.getByRole("button", { name: "Agent options" }).first().click();
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
}

test("agent lifecycle: create (declarative + harness) then delete", async ({ page, mock }) => {
  await test.step("creates a fully-configured declarative agent", async () => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill("e2e-full-agent");
    await page.getByLabel("Description").fill("Fully configured e2e agent");
    // System prompt is pre-filled; namespace/type default to default/Declarative.
    await selectOption(page, "#agent-field-model", MODEL_OPTION);

    // Tools — open the dialog, pick the seeded tool, confirm.
    await page.getByRole("button", { name: "Add Tools & Agents" }).click();
    await page.getByTestId("tool-row-e2e-tool-server-e2e-tool").click();
    await page.getByRole("button", { name: /Save Selection/ }).click();

    // Long-term memory — embedding model + TTL (memory only emits when a model is set).
    await selectOption(page, "#agent-field-memory-model", MODEL_OPTION);
    await page.locator("#agent-field-memory-ttl").fill("30");

    // Context — enable event compaction.
    await page.getByTestId("context-compaction-switch").click();

    // Skills — one OCI image ref.
    await page.locator("#agent-oci-skill-0").fill("ghcr.io/example/python-skill:v1.0.0");

    await page.getByRole("button", { name: "Create Agent" }).click();
    // Success = redirect to the agents list (no toast).
    await expect(page).toHaveURL(/\/agents(\?|$)/);

    const req = await mock.lastRequest<{
      metadata: { name: string; namespace: string };
      spec: {
        type: string;
        description: string;
        declarative: {
          modelConfig: string;
          tools: Array<{ type: string; mcpServer?: { toolNames: string[] } }>;
          memory?: { modelConfig: string; ttlDays?: number };
          context?: { compaction?: unknown };
        };
        skills?: { refs?: string[] };
      };
    }>("POST", "/api/agents");
    expect(req, "expected a captured POST /api/agents").not.toBeNull();
    const spec = req!.body.spec;
    expect(req!.body.metadata.name).toBe("e2e-full-agent");
    expect(spec.type).toBe("Declarative");
    expect(spec.description).toBe("Fully configured e2e agent");
    // modelConfig is the name only — the "default/" namespace is stripped.
    expect(spec.declarative.modelConfig).toBe("default-model-config");
    // Tools → spec.declarative.tools[]
    expect(spec.declarative.tools?.[0]?.type).toBe("McpServer");
    expect(spec.declarative.tools[0].mcpServer?.toolNames).toContain("e2e-tool");
    // Memory → spec.declarative.memory
    expect(spec.declarative.memory?.modelConfig).toBe("default-model-config");
    expect(spec.declarative.memory?.ttlDays).toBe(30);
    // Context → spec.declarative.context
    expect(spec.declarative.context?.compaction).toBeTruthy();
    // Skills → spec.skills (top-level)
    expect(spec.skills?.refs).toContain("ghcr.io/example/python-skill:v1.0.0");
  });

  await test.step("creates a harness (BYO) agent — default backend", async () => {
    // Minimal harness = name + model; namespace defaults to "default", backend to openclaw.
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

  await test.step("creates a Hermes-backed harness", async () => {
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

  await test.step("deletes an agent via the confirm dialog", async () => {
    // Delete is reachable via the per-card "Agent options" menu → "Delete" →
    // confirm dialog, issuing DELETE /api/agents/<ns>/<name> (captured server-side).
    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText("e2e-agent")).toBeVisible();

    await openDeleteDialog(page);
    await page.getByRole("alertdialog").getByRole("button", { name: "Delete" }).click();

    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    const req = await mock.lastRequest("DELETE", "/api/agents/default/e2e-agent");
    expect(req, "expected a captured DELETE /api/agents/<ns>/<name>").not.toBeNull();
  });
});

test("agent failures: validation blocks, error toasts, and delete-cancel", async ({ page, mock }) => {
  await test.step("declarative create blocks submit + shows validation errors when required fields are empty", async () => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByRole("button", { name: "Create Agent" }).click();

    // Client-side validation blocks the submit: required-field errors render and
    // the model field is flagged invalid — no navigation, no request sent.
    await expect(page.getByText("Description is required")).toBeVisible();
    await expect(page.getByText("Please select a model")).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new/);
    expect((await mock.capturedRequests()).filter((r) => r.method === "POST")).toHaveLength(0);
  });

  await test.step("declarative create shows an error toast when the create request fails", async () => {
    await mock.reset();
    await mock.agentsCreateError();
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill("e2e-fail-agent");
    await page.getByLabel("Description").fill("will fail");
    await selectOption(page, "#agent-field-model", MODEL_OPTION);
    await page.getByRole("button", { name: "Create Agent" }).click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new/);
  });

  await test.step("harness create blocks submit when required fields are empty", async () => {
    await mock.reset();
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page).toHaveURL(/\/agents\/new-harness/);
    expect((await mock.capturedRequests()).filter((r) => r.method === "POST")).toHaveLength(0);
  });

  await test.step("harness create shows an error toast when create fails", async () => {
    await mock.reset();
    await mock.setMutation("POST", "/api/agentharnesses", { status: 500, body: { error: "boom" } });
    await loadPage(page, "/agents/new-harness", { heading: "New Agent Harness" });

    await page.getByLabel("Agent name").fill("e2e-fail-harness");
    await selectOption(page, "#agent-field-model", MODEL_OPTION);
    await page.getByRole("button", { name: "Create harness" }).click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new-harness/);
  });

  await test.step("delete-cancel leaves the agent untouched", async () => {
    await mock.reset();
    await loadPage(page, "/", { heading: "Agents" });

    await openDeleteDialog(page);
    await page.getByRole("alertdialog").getByRole("button", { name: "Cancel" }).click();

    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    const deletes = (await mock.capturedRequests()).filter((r) => r.method === "DELETE");
    expect(deletes).toHaveLength(0);
  });
});
