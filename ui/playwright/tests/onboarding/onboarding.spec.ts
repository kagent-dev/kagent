import { test, expect } from "../../fixtures/test";

// Onboarding wizard — completion journey + skip, against the real backend. The
// wizard shows when localStorage['kagent-onboarding'] !== "true"; the shared
// fixture sets it to "true", so each test overrides it to "false" (init scripts
// run in registration order, so this wins). With the seeded model config present,
// step 1 defaults to "select existing", avoiding the create-a-provider path.
//
// Completing the wizard creates a real agent, so we give it a unique name and
// delete it afterward. Creating a model in the wizard is covered by the models
// suite, so this walks the "select existing model" path.

const NAMESPACE = "kagent";

test("onboarding: complete the wizard", async ({ page }, testInfo) => {
  const agentName = `e2e-onboard-${Date.now().toString(36)}-${testInfo.retry}`;
  const ref = `${NAMESPACE}/${agentName}`;

  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));
  await page.goto("/");

  // region Creating — walk the wizard end to end to create an agent
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

  await test.step("step 2 — agent setup with a unique name", async () => {
    await expect(page.getByText("Step 2: Set Up The AI Agent")).toBeVisible();
    await page.getByLabel("Agent Name", { exact: true }).fill(agentName);
    await page.getByRole("button", { name: "Next: Select Tools" }).click();
  });

  await test.step("step 3 — tools are optional", async () => {
    await expect(page.getByText("Step 3: Select Tools")).toBeVisible();
    await page.getByRole("button", { name: "Next: Review" }).click();
  });

  await test.step("step 4 — review + finalize (creates the agent)", async () => {
    await expect(page.getByText("Step 4: Review Agent Configuration")).toBeVisible();
    await page.getByRole("button", { name: /Finish/ }).click();
  });

  await test.step("step 5 — lands on the agents list with the new agent", async () => {
    await expect(page.getByText("Setup Complete!")).toBeVisible();
    await page.getByRole("button", { name: /Go to Agent/ }).click();

    await expect(page.getByText("Setup Complete!")).toHaveCount(0);
    await expect(page.getByRole("heading", { level: 1, name: "Agents" })).toBeVisible();
    // The agent the wizard created is present in the list it lands on.
    await expect(page.getByText(agentName).first()).toBeVisible();
    expect(await page.evaluate(() => window.localStorage.getItem("kagent-onboarding"))).toBe("true");
  });

  // region Deleting — remove the agent the wizard created
  await test.step("cleans up the agent the wizard created", async () => {
    await page.getByTestId(`agent-options-${ref}`).first().click();
    await page.getByRole("menuitem", { name: "Delete" }).click();
    const dialog = page.getByRole("alertdialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Delete" }).click();
    await expect(page.getByText(agentName)).toHaveCount(0);
  });
});

test("onboarding: skip the wizard", async ({ page }) => {
  // region Skipping — dismiss the wizard without creating anything
  await page.addInitScript(() => window.localStorage.setItem("kagent-onboarding", "false"));
  await page.goto("/");

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toBeVisible();
  await page.getByRole("button", { name: /Skip wizard/ }).click();

  await expect(page.getByText("Bringing Agentic AI to Cloud Native")).toHaveCount(0);
  expect(await page.evaluate(() => window.localStorage.getItem("kagent-onboarding"))).toBe("true");
});
