import { test, expect } from "@playwright/test";
import {
  dataRows,
  expectNoLoadFailure,
  liveRoutes,
  loadLive,
  rowNamed,
  throwawayName,
} from "./helpers/live";

/**
 * Creating and deleting an agent, on a real cluster, through the UI.
 *
 * This is the journey the mock suite cannot vouch for. Both halves of it were
 * broken against a real controller while the mock suite was green: the form sent
 * the model as a `namespace/name` ref where the controller wanted a bare name, so
 * every create was rejected; and the page read the created agent out of a wrapper
 * the create response does not have, so a create that *had* worked reported failure
 * and stayed on the form. Neither could be seen without a cluster.
 *
 * The agent is deleted in teardown as well as in the spec body, because a run that
 * dies midway would otherwise leave a real resource behind.
 */

const AGENT = throwawayName("agent");
const NAMESPACE = "kagent";

test.afterEach(async ({ request, baseURL }) => {
  // Deleting through the API rather than the UI: teardown runs after a failure, when
  // the page may be anywhere at all, and a cleanup that depends on the UI working is
  // exactly the cleanup that fails when the UI does not.
  const response = await request.delete(
    `${baseURL}/api/agents/${NAMESPACE}/${AGENT}`,
    { failOnStatusCode: false },
  );
  // 404 is the goal state: either the spec deleted it, or it was never created.
  expect(
    [200, 204, 404],
    `teardown could not remove ${NAMESPACE}/${AGENT} (${response.status()})`,
  ).toContain(response.status());
});

test("live: an agent can be created and deleted through the UI", async ({ page }) => {
  await test.step("1. the create form opens with the cluster's models offered", async () => {
    await loadLive(page, liveRoutes.agentNew);
    await expectNoLoadFailure(page);

    await page.getByTestId("agent-form-name").fill(AGENT);
    await page.getByTestId("agent-form-namespace").fill(NAMESPACE);
    await page
      .getByTestId("agent-form-description")
      .fill("Created by the live end-to-end suite; safe to delete.");
    // Required, and the form says so rather than letting a create fail at the API —
    // which is the client-side validation `playwright/DEFERRED.md` records as lost
    // coverage. It is not lost; this spec relies on it.
    await page
      .getByTestId("agent-form-system-message")
      .fill("You are a test agent created by an end-to-end run. Answer briefly.");

    // The options come from the cluster's own ModelConfigs, so an empty list here is
    // a real failure rather than a slow render: the install ships one.
    await page.getByTestId("agent-form-model").click();
    const option = page.locator(".ant-select-item-option").first();
    await expect(option, "the cluster offered no model configurations").toBeVisible({
      timeout: 30_000,
    });
    await option.click();
  });

  await test.step("2. submitting reaches the controller and reports success", async () => {
    await page.getByTestId("agent-form-submit").click();

    // Success is leaving the form. A create that failed keeps the user on it with an
    // error, which is the shape the earlier defect produced for an agent that had in
    // fact been created.
    await expect(page).toHaveURL(/\/agents$/, { timeout: 60_000 });
  });

  await test.step("3. the agent is listed, read back from the cluster", async () => {
    await expectNoLoadFailure(page);
    await expect(rowNamed(page, AGENT)).toHaveCount(1, { timeout: 60_000 });
  });

  await test.step("4. deleting it asks first, and names what it will delete", async () => {
    const before = await dataRows(page).count();

    await page.getByTestId(`delete-${AGENT}`).click();
    const confirm = page.locator(".ant-popconfirm").filter({ hasText: AGENT });
    await expect(confirm, "the confirmation did not name the agent").toBeVisible();

    await page.getByRole("button", { name: "Delete", exact: true }).last().click();

    await test.step("and the row is gone from the list a reader looks at", async () => {
      await expect(rowNamed(page, AGENT)).toHaveCount(0, { timeout: 60_000 });
      await expect(dataRows(page)).toHaveCount(before - 1);
    });
  });

  await test.step("5. the cluster agrees it is gone", async () => {
    // The list could be stale; the controller cannot be. This is what makes the
    // previous step evidence of a delete rather than of a re-render.
    const response = await page.request.get(
      `/api/agents/${NAMESPACE}/${AGENT}`,
      { failOnStatusCode: false },
    );
    expect(response.status()).toBe(404);
  });
});
