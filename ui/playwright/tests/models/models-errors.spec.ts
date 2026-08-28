import { test, expect } from "../../fixtures/test";
import { dataRows, loadPage, rowNamed, routes } from "../../helpers/app";
import { operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * Models — the error journey.
 *
 * Same shape as the agents error journey, and deliberately so: a failed list
 * load should look and behave the same wherever it happens, and two pages
 * asserting the same contract is what will catch the first one that drifts.
 */

test("models: a failed load is reported, not disguised as an empty list", async ({
  page,
}) => {
  await test.step("1. the failure is on screen and names what went wrong", async () => {
    await loadPage(page, routes.models, { scenario: "error", title: "Models" });

    const alert = page.getByTestId("models-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load model configurations");
    // The backend's own account of the failure reaches the user rather than a
    // generic message, and it names the call that failed — which the HTTP status
    // this used to assert never did. Asserted as that property, not as a literal
    // status: there is no status to report now, and putting one back in the
    // message to satisfy a string match would be fitting the product to a stale
    // test.
    await expect(alert).toContainText("asked to fail");
    await expect(alert).toContainText("ModelService/ListModelConfigs");
  });

  await test.step("2. it is not mistaken for an empty list", async () => {
    await expect(page.getByText("No model configurations yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. retrying asks the backend again", async () => {
    const before = await operationCalls(page, rpc.listModelConfigs);

    await page.getByRole("button", { name: "Try again" }).click();
    await expect
      .poll(() => operationCalls(page, rpc.listModelConfigs), { timeout: 10_000 })
      .toBeGreaterThan(before);
    await expect(page.getByTestId("models-error")).toBeVisible();
  });

  await test.step("4. the page recovers once the backend does", async () => {
    await loadPage(page, routes.models, { scenario: "ok", title: "Models" });
    await expect(page.getByTestId("models-error")).toHaveCount(0);
    await expect(rowNamed(page, "default-model-config")).toHaveCount(1);
  });
});
