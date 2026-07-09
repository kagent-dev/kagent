import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";
import { selectOption } from "../../helpers/select";

// Sub-stage 2.1 — the declarative create-agent flow (single-page form).
// The model dropdown reads /api/modelconfigs (seeded), and submit does a
// server-side POST /api/agents, which the stub captures for payload assertions.

// Model option label = `${spec.model} (${ref})` for the seeded modelconfig.
const MODEL_OPTION = "gpt-4o (default/default-model-config)";
const AGENT_NAME = "e2e-new-agent";
const AGENT_DESC = "Created by the Playwright e2e suite";

test.describe("create agent (declarative)", () => {
  test("creates an agent and POSTs the expected payload", async ({ page, mock }) => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill(AGENT_NAME);
    await page.getByLabel("Description").fill(AGENT_DESC);
    // System prompt is pre-filled with the default; namespace/type default to
    // "default"/"Declarative" — only the model must be picked.
    await selectOption(page, "#agent-field-model", MODEL_OPTION);

    await page.getByRole("button", { name: "Create Agent" }).click();

    // Success = redirect to the agents list (no toast).
    await expect(page).toHaveURL(/\/agents(\?|$)/);
    await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toBeVisible();

    // Assert the payload the stub captured server-side.
    const req = await mock.lastRequest<{
      metadata: { name: string; namespace: string };
      spec: { type: string; description: string; declarative: { modelConfig: string } };
    }>("POST", "/api/agents");
    expect(req, "expected a captured POST /api/agents").not.toBeNull();
    expect(req!.body.metadata.name).toBe(AGENT_NAME);
    expect(req!.body.spec.type).toBe("Declarative");
    expect(req!.body.spec.description).toBe(AGENT_DESC);
    // modelConfig is the name only — the "default/" namespace is stripped.
    expect(req!.body.spec.declarative.modelConfig).toBe("default-model-config");
  });

  test("blocks submit and shows a validation error when required fields are empty", async ({ page, mock }) => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByRole("button", { name: "Create Agent" }).click();

    // Client-side validation blocks the submit: required-field errors render and
    // the model field is flagged invalid — no navigation, no request sent.
    await expect(page.getByText("Description is required")).toBeVisible();
    await expect(page.getByText("Please select a model")).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new/);
    expect(await mock.capturedRequests()).toHaveLength(0);
  });

  test("shows an error toast when the create request fails", async ({ page, mock }) => {
    await mock.agentsCreateError();
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill("e2e-fail-agent");
    await page.getByLabel("Description").fill("will fail");
    await selectOption(page, "#agent-field-model", MODEL_OPTION);
    await page.getByRole("button", { name: "Create Agent" }).click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    await expect(page).toHaveURL(/\/agents\/new/);
  });

  // Exercises the remaining form sections (Tools, Long-term memory, Context,
  // Skills) — none of which the minimal path or Storybook/Chromatic covers — and
  // asserts each lands in the submitted CR.
  test("creates a fully-configured agent (tools, memory, context, skills)", async ({ page, mock }) => {
    await loadPage(page, "/agents/new", { heading: "New Agent" });

    await page.getByLabel("Agent name").fill("e2e-full-agent");
    await page.getByLabel("Description").fill("Fully configured e2e agent");
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
    await expect(page).toHaveURL(/\/agents(\?|$)/);

    const req = await mock.lastRequest<{
      spec: {
        declarative: {
          tools: Array<{ type: string; mcpServer?: { toolNames: string[] } }>;
          memory?: { modelConfig: string; ttlDays?: number };
          context?: { compaction?: unknown };
        };
        skills?: { refs?: string[] };
      };
    }>("POST", "/api/agents");
    expect(req, "expected a captured POST /api/agents").not.toBeNull();
    const spec = req!.body.spec;

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
});
