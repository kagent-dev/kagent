import { test, expect } from "../../fixtures/test";

// Sub-stage 2.8 — complete the first-run onboarding wizard end to end. The
// wizard shows when localStorage['kagent-onboarding'] !== "true"; the shared
// fixture sets it to "true", so we override it to "false" here (init scripts run
// in registration order, so this wins). With a seeded modelconfig, step 1
// defaults to "select existing", avoiding the create-a-provider path. Finish
// POSTs /api/agents and flips the flag to "true".
test("completes the onboarding wizard", async ({ page, mock }) => {
  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));

  await page.goto("/");

  // Welcome
  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toBeVisible();
  await page.getByRole("button", { name: /Let's Get Started/ }).click();

  // Step 1 — select the seeded existing model config
  await expect(page.getByText("Step 1: Configure AI Model")).toBeVisible();
  await page.getByRole("combobox").first().click();
  await page.getByRole("option").first().click();
  await page.getByRole("button", { name: "Next: Agent Setup" }).click();

  // Step 2 — fields are prefilled
  await expect(page.getByText("Step 2: Set Up The AI Agent")).toBeVisible();
  await page.getByRole("button", { name: "Next: Select Tools" }).click();

  // Step 3 — tools are optional (seeded list is non-empty so Next is enabled)
  await expect(page.getByText("Step 3: Select Tools")).toBeVisible();
  await page.getByRole("button", { name: "Next: Review" }).click();

  // Step 4 — review + finalize (POSTs /api/agents)
  await expect(page.getByText("Step 4: Review Agent Configuration")).toBeVisible();
  await page.getByRole("button", { name: /Finish/ }).click();

  // Step 5 — done
  await expect(page.getByText("Setup Complete!")).toBeVisible();
  await page.getByRole("button", { name: /Go to Agent/ }).click();

  // Wizard dismissed → normal app content, and the flag is persisted.
  await expect(page.getByText("Setup Complete!")).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toBeVisible();
  expect(await page.evaluate(() => window.localStorage.getItem("kagent-onboarding"))).toBe("true");

  const req = await mock.lastRequest("POST", "/api/agents");
  expect(req, "expected a captured POST /api/agents at finish").not.toBeNull();
});

test("creates a new model config during onboarding (create path)", async ({ page, mock }) => {
  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));
  await page.goto("/");

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toBeVisible();
  await page.getByRole("button", { name: /Let's Get Started/ }).click();

  await expect(page.getByText("Step 1: Configure AI Model")).toBeVisible();
  await page.getByRole("radio", { name: "Create New" }).click();

  // Provider/model via the ModelProviderCombobox, then API key.
  await page.getByRole("combobox").first().click();
  await page.getByRole("option", { name: /gpt-4o/ }).first().click();
  await page.getByPlaceholder("Enter your API key").fill("sk-onboard");
  await page.getByRole("button", { name: "Create & Continue" }).click();

  await expect(page.getByText("Step 2: Set Up The AI Agent")).toBeVisible();
  expect(await mock.lastRequest("POST", "/api/modelconfigs"), "expected POST /api/modelconfigs").not.toBeNull();
});

test("skips the wizard", async ({ page }) => {
  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));
  await page.goto("/");

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toBeVisible();
  await page.getByRole("button", { name: /Skip wizard/ }).click();

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toHaveCount(0);
  expect(await page.evaluate(() => window.localStorage.getItem("kagent-onboarding"))).toBe("true");
});
