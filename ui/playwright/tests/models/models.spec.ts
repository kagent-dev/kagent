import { test, expect } from "../../fixtures/test";
import {
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
} from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  expectLoading,
  chooseFilter,
  clickRefresh,
  expectRequired,
} from "../../helpers/resource";
import { operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * Model configurations, on the fixtures.
 *
 * The write journey is not here: creating one, reading it back, changing its credential
 * and deleting it runs against both backends from `shared/models/`. What is left is what
 * only fixed data can settle — the seeded rows and their refs taken apart, the refresh
 * confirmation, the filter in the address, the required-field marks, and the empty and
 * failure states.
 */

/** The four seeded configurations, which is what "nothing narrowed" has to mean. */
const SEEDED = [
  "default-model-config",
  "anthropic-model-config",
  "ollama-local",
  "bedrock-haiku",
];

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("models: the list reads, narrows, and says what the form requires", async ({
  page,
}) => {
  await test.step("1. a loading state precedes the data", async () => {
    await expectLoading(page, routes.models);
  });

  await test.step("2. every configuration is listed, with its ref taken apart", async () => {
    for (const name of SEEDED) {
      await expect(rowNamed(page, name), `"${name}" is missing`).toHaveCount(1);
    }
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
    await expectSettled(page);

    // The API returns one `namespace/name` string; the list has to take it apart to
    // fill two columns, which is the part worth pinning.
    const openai = rowNamed(page, "default-model-config");
    await expect(openai).toContainText("kagent");
    await expect(openai).toContainText("OpenAI");
    await expect(openai).toContainText("gpt-4.1");

    const ollama = rowNamed(page, "ollama-local");
    await expect(ollama).toContainText("platform");
    await expect(ollama).toContainText("Ollama");
    // No API key secret on a local provider — the column shows a dash, not blank.
    await expect(ollama).toContainText("—");
  });

  await test.step("3. refreshing re-reads the list, and says that it did", async () => {
    /*
     * Back on `ok` first, and that is the point rather than housekeeping: the scenario
     * persists for the browsing session, so a refresh clicked while `slow` is still in
     * force waits 2.5 seconds per call before it can confirm anything — which reads as
     * a missing feature rather than a slow one. The prompts journey failed exactly that
     * way, on both engines, because its list fans out one call per namespace.
     */
    await loadPage(page, routes.models, { scenario: "ok", title: "Models" });
    await expectSettled(page);

    // Operations, not requests: under the substituted transport a working refresh
    // makes no HTTP request for `page.on("request")` to see.
    const before = await operationCalls(page, rpc.listModelConfigs);

    await clickRefresh(page);
    await expect
      .poll(() => operationCalls(page, rpc.listModelConfigs), { timeout: 10_000 })
      .toBeGreaterThan(before);

    await expectSettled(page);
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });

  await test.step("4. narrowing the list is an address, so it survives a reload", async () => {
    await expect(page.getByTestId("models-filters-pills")).toHaveCount(0);

    await chooseFilter(page, "models-filters-filter-ns", "kagent");
    // The wiring, in one assertion: a page that rendered the bar and never passed the
    // selection to its own read would keep all four rows.
    await expect(dataRows(page)).toHaveCount(2);
    await expect(page).toHaveURL(/[?&]ns=kagent(&|$)/);

    await page.getByTestId("models-filters-search").fill("config");
    await expect(page).toHaveURL(/[?&]q=config(&|$)/);
    // Both writes survived each other. They landed in one tick once and the second
    // read the address as it was before the first, which put the cleared filter back.
    await expect(page).toHaveURL(/[?&]ns=kagent(&|$)/);

    // Sorting goes into the address too. Ascending is the default direction and is
    // deliberately not written, so the address carries a direction only where one was
    // chosen.
    await page.getByRole("columnheader", { name: "Name", exact: true }).click();
    await expect(page).toHaveURL(/sort=name/);
    await expect(page).not.toHaveURL(/dir=/);

    await page.reload();
    await expectSettled(page);
    // The controls, not just the parameters: a page that kept the URL and rendered the
    // unfiltered list would pass a URL-only assertion and be broken.
    await expect(page.getByTestId("models-filters-search")).toHaveValue("config");
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(2);
    await expect(
      page.locator("th.ant-table-column-sort").filter({ hasText: "Name" }),
    ).toHaveCount(1);

    // And the same address typed fresh gives the same view, which is what a link is.
    // Opened cold rather than reloaded, so nothing in memory can be carrying the state.
    await page.goto("/models?mock=ok&q=config&ns=kagent&sort=namespace&dir=desc");
    await expectSettled(page);
    await expect(page.getByTestId("models-filters-search")).toHaveValue("config");
    await expect(page.getByTestId("models-filters-pill-ns-kagent")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(2);
  });

  await test.step("5. the create form marks what it will not submit without", async () => {
    await loadPage(page, routes.models, { title: "Models" });
    await page.getByTestId("models-new").click();
    await page.waitForURL(/\/models\/new(\?|$)/);
    await expectSettled(page);

    /*
     * The mark and the gate are two separate statements about the same field: antd
     * draws the asterisk from `required` on a `Form.Item`, while `modelDraftIssues`
     * gates the submit in code. Nothing but this keeps them agreeing, and they had
     * already come apart — the API key was required to create with nothing on screen
     * saying so.
     *
     * Asserted as a pair, marked *and* unmarked: a test that only checked the marks
     * would pass on a form that marked every field.
     */
    await expectRequired(page, {
      marked: ["Provider", "Model", "Name", "Namespace", "API key"],
      // A radio group that arrives with a choice already made cannot be missing one.
      unmarked: ["Authentication"],
    });
  });

  await test.step("6. an empty result says so instead of showing a bare table", async () => {
    await loadPage(page, routes.models, { scenario: "empty", title: "Models" });
    await expect(page.getByText("No model configurations yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("7. a failed load is reported, not disguised as an empty list", async () => {
    await loadPage(page, routes.models, { scenario: "error", title: "Models" });

    const alert = page.getByTestId("models-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load model configurations");
    // The backend's own account of the failure reaches the reader rather than a
    // generic message, and it names the call that failed — which an HTTP status never
    // did. Asserted as that property rather than as a literal status: a gRPC error is
    // an HTTP 200, so there is no status to report.
    await expect(alert).toContainText("asked to fail");
    await expect(alert).toContainText("ModelService/ListModelConfigs");

    // The distinction this step exists for: "there are none" and "we could not find
    // out" lead a reader to opposite conclusions, and only one of them is true.
    await expect(page.getByText("No model configurations yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("8. retrying asks the backend again, and it recovers", async () => {
    const before = await operationCalls(page, rpc.listModelConfigs);

    await page.getByRole("button", { name: "Try again" }).click();
    await expect
      .poll(() => operationCalls(page, rpc.listModelConfigs), { timeout: 10_000 })
      .toBeGreaterThan(before);
    // Still failing, so the message stays put rather than flickering away.
    await expect(page.getByTestId("models-error")).toBeVisible();

    // A failure that cannot clear is indistinguishable from a broken page, so the
    // recovery is as much a part of the contract as the message.
    await loadPage(page, routes.models, { scenario: "ok", title: "Models" });
    await expect(page.getByTestId("models-error")).toHaveCount(0);
    await expect(rowNamed(page, "default-model-config")).toHaveCount(1);
  });
});
