import { test, expect } from "../../fixtures/test";
import { dataRows, loadPage, rowNamed, routes } from "../../helpers/app";
import { operationCallCounts, rpc } from "../../helpers/mockCalls";

// No console allowance any more. This spec asks for a library that does not exist, and
// that used to be an HTTP 404 the browser logged; the API is gRPC over a substituted
// transport now, so nothing is fetched and nothing is logged. The allowance is left off
// deliberately rather than kept "just in case": one that forgives noise that can no
// longer occur reads as evidence that it still does.

/**
 * Prompt libraries — the error journeys, list and detail.
 *
 * Two failures rather than one, because they are different: a list that cannot load
 * and a *single* library that cannot be found lead a reader to different actions, and
 * a page that answered both the same way would be wrong about one of them.
 */

test("prompts: a failed load is reported, not disguised as an empty list", async ({
  page,
}) => {
  await test.step("1. the failure is on screen and names what went wrong", async () => {
    await loadPage(page, routes.prompts, { scenario: "error", title: "Prompts" });

    const alert = page.getByTestId("prompts-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load prompt libraries");
    // The backend's own account of the failure reaches the user rather than a
    // generic message, and it names the call that failed — which the HTTP status
    // this used to assert never did. Asserted as that property, not as a literal
    // status: there is no status to report now, and putting one back in the
    // message to satisfy a string match would be fitting the product to a stale
    // test.
    await expect(alert).toContainText("asked to fail");
  });

  await test.step("2. it is not mistaken for an empty list", async () => {
    await expect(page.getByText("No prompt libraries yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. retrying asks the backend again", async () => {
    // Either read counts as the retry. Listing across namespaces starts with the
    // namespaces call and fans out from there, so under a failing backend the retry
    // does not reach `ListPromptTemplates` at all — it fails at the first hop.
    // Watching only that one would report "no retry happened" when a retry
    // demonstrably did.
    const WATCHED = [rpc.listPromptTemplates, rpc.listNamespaces] as const;
    const total = async () => {
      const counts = await operationCallCounts(page, WATCHED);
      return WATCHED.reduce((sum, read) => sum + counts[read], 0);
    };

    const before = await total();
    await page.getByRole("button", { name: "Try again" }).click();
    await expect.poll(total, { timeout: 10_000 }).toBeGreaterThan(before);
  });

  await test.step("4. the page recovers when the backend does", async () => {
    await loadPage(page, routes.prompts, { title: "Prompts" });
    await expect(page.getByTestId("prompts-error")).toHaveCount(0);
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(1);
  });
});

test("prompts: a library that fails to load says so on its own page", async ({ page }) => {
  await test.step("1. the detail page reports its own failure", async () => {
    await page.goto("/prompts/kagent/shared-fragments?mock=error");

    const alert = page.getByTestId("prompt-detail-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load this prompt library");
  });

  await test.step("2. no fragments are shown alongside the failure", async () => {
    // Showing a partial fragment list under an error would invite copying an include
    // for something that may not exist.
    await expect(page.getByTestId("prompt-fragments")).toHaveCount(0);
  });

  await test.step("3. a library that does not exist is told apart from one that failed", async () => {
    // `?mock=ok` explicitly: the scenario persists across navigation, so without it
    // this step inherits the failure from step 1 and gets a 500 where it needs a 404
    // — which would make a passing page look broken.
    await page.goto("/prompts/kagent/no-such-library?mock=ok");
    // Different state, different message: nothing is wrong with the backend here, so
    // reporting a failure would send a reader looking for a fault that is not there.
    await expect(page.getByTestId("prompt-detail-not-found")).toBeVisible();
    await expect(page.getByTestId("prompt-detail-error")).toHaveCount(0);
  });
});
