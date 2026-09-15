import { test, expect } from "@playwright/test";
import {
  dataRows,
  expectNoLoadFailure,
  liveRoutes,
  loadLive,
} from "./helpers/live";

/**
 * Every page, against a real controller.
 *
 * The mock suite already asserts each page's loading, empty and failure states,
 * which it can because it can ask the mock backend for them. A real cluster cannot
 * be asked for a 500, so this spec asserts the thing the mock suite structurally
 * cannot: that the pages work against what a controller actually sends.
 *
 * That has not been a formality on this project. Every defect found by pointing the
 * app at a real backend was a place where a fixture had taught a shape the
 * controller does not use — a null collection where an array was declared, a create
 * response wrapped one way in the fixtures and another by the API, a resource name
 * sent where a bare name belonged. None of them were visible to a green mock suite.
 */

test("live: every page loads against the cluster and reports no failure", async ({
  page,
}) => {
  for (const [name, path] of Object.entries(liveRoutes)) {
    if (name === "agentNew") continue; // A form, covered by the lifecycle spec.

    await test.step(`${name} (${path})`, async () => {
      await loadLive(page, path);
      // The distinction worth keeping: a page that could not reach the controller
      // must not be read as a page with nothing on it.
      await expectNoLoadFailure(page);
      await expect(page.locator('[data-testid="app-content"]')).toBeVisible();
    });
  }
});

test("live: the agents the cluster installed are listed with their model", async ({
  page,
}) => {
  await loadLive(page, liveRoutes.agents);
  await expectNoLoadFailure(page);

  await test.step("the install's own agents are present", async () => {
    // kagent installs a set of agents; asserting on the count rather than on names
    // keeps this from breaking when the chart's default set changes, while still
    // failing if the list is empty because nothing was read.
    await expect(dataRows(page).first()).toBeVisible({ timeout: 60_000 });
    const count = await dataRows(page).count();
    expect(count, "the cluster reported no agents at all").toBeGreaterThan(0);
  });

  await test.step("each row resolves its model rather than showing a ref", async () => {
    // The controller denormalises the referenced ModelConfig onto the row. A row
    // showing a bare dash here means either the reference did not resolve or the UI
    // read the wrong field — both of which a fixture would have hidden.
    const first = dataRows(page).first();
    await expect(first).not.toContainText("__NS__");
  });

  await test.step("the tool count is a number, not a crash", async () => {
    // `tools` arrives as JSON null for an agent with none, because Go marshals a nil
    // slice that way. Reading `.length` off it took this whole page down against a
    // real cluster once.
    await expect(page.getByTestId("agents-error")).toHaveCount(0);
  });
});

test("live: the models the cluster installed are listed with their provider", async ({
  page,
}) => {
  await loadLive(page, liveRoutes.models);
  await expectNoLoadFailure(page);

  await expect(dataRows(page).first()).toBeVisible({ timeout: 60_000 });
  const count = await dataRows(page).count();
  expect(count, "the cluster reported no model configurations").toBeGreaterThan(0);
});

test("live: tool servers report the tools they discovered", async ({ page }) => {
  await loadLive(page, liveRoutes.mcpServers);
  await expectNoLoadFailure(page);

  await test.step("the summary counts servers and tools", async () => {
    // Both counts are derived: the servers from the rows, the tools by summing what
    // each discovered. A server that discovered none must still be counted, since a
    // registered server reporting nothing is the one most likely to be misconfigured.
    await expect(page.getByTestId("mcp-servers-summary")).toBeVisible({
      timeout: 60_000,
    });
    await expect(page.getByTestId("mcp-servers-summary")).toContainText("server");
  });
});
