import { test, expect } from "../../fixtures/test";

// Onboarding wizard journey + variants. The wizard shows when
// localStorage['kagent-onboarding'] !== "true"; the shared fixture sets it to
// "true", so each test overrides it to "false" (init scripts run in registration
// order, so this wins). With a seeded modelconfig, step 1 defaults to "select
// existing", avoiding the create-a-provider path. Finish POSTs /api/agents and
// flips the flag to "true".
//
// The journey walks the whole "select existing model" wizard, phased by step.
// The create-a-new-model branch and the skip path stay as their own tests.

test("onboarding wizard: completes end to end with an existing model", async ({ page, mock }) => {
  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));
  await page.goto("/");

  await test.step("welcome → get started", async () => {
    await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toBeVisible();
    await page.getByRole("button", { name: /Let's Get Started/ }).click();
  });

  await test.step("step 1 — select the seeded existing model config", async () => {
    await expect(page.getByText("Step 1: Configure AI Model")).toBeVisible();
    await page.getByRole("combobox").first().click();
    await page.getByRole("option").first().click();
    await page.getByRole("button", { name: "Next: Agent Setup" }).click();
  });

  await test.step("step 2 — agent setup (prefilled)", async () => {
    await expect(page.getByText("Step 2: Set Up The AI Agent")).toBeVisible();
    await page.getByRole("button", { name: "Next: Select Tools" }).click();
  });

  await test.step("step 3 — tools are optional", async () => {
    // Seeded list is non-empty so Next is enabled.
    await expect(page.getByText("Step 3: Select Tools")).toBeVisible();
    await page.getByRole("button", { name: "Next: Review" }).click();
  });

  await test.step("step 4 — review + finalize (POSTs /api/agents)", async () => {
    await expect(page.getByText("Step 4: Review Agent Configuration")).toBeVisible();
    await page.getByRole("button", { name: /Finish/ }).click();
  });

  await test.step("step 5 — done, wizard dismissed and flag persisted", async () => {
    await expect(page.getByText("Setup Complete!")).toBeVisible();
    await page.getByRole("button", { name: /Go to Agent/ }).click();

    await expect(page.getByText("Setup Complete!")).toHaveCount(0);
    await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toBeVisible();
    expect(await page.evaluate(() => window.localStorage.getItem("kagent-onboarding"))).toBe("true");

    const req = await mock.lastRequest("POST", "/api/agents");
    expect(req, "expected a captured POST /api/agents at finish").not.toBeNull();
  });
});

test("onboarding: creates a new model config during the wizard (create path)", async ({ page, mock }) => {
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

test("onboarding: skips the wizard", async ({ page }) => {
  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));
  await page.goto("/");

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toBeVisible();
  await page.getByRole("button", { name: /Skip wizard/ }).click();

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toHaveCount(0);
  expect(await page.evaluate(() => window.localStorage.getItem("kagent-onboarding"))).toBe("true");
});
