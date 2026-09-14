import { test, expect } from "../../fixtures/test";
import { loadPage, routes } from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  confirmDelete,
  expectRequired,
} from "../../helpers/resource";

/**
 * Harnesses — the whole life of one, in a single journey.
 *
 * One test rather than several because a video and a trace are recorded per test: a
 * lifecycle split across two of them is one you have to reassemble from two recordings,
 * neither of which shows that the harness the second deletes is the one the first made.
 *
 * **There is no update half.** The tab offers create and delete and no edit, so the
 * journey is create, read back, remove. That is narrower than `HarnessService`, which
 * implements update too — the application has never called it. Worth stating because the
 * opposite mistake was already made here once: this tab was read-only on a note claiming
 * the service was, and the service never was.
 *
 * ## What the tab exists to say
 *
 * **The admission selector has to be visible.** A harness admits templates through a
 * label selector, and that selector is what decides whether a template ever becomes an
 * agent at all. A template carrying no label it matches saves happily and then does
 * nothing, with nothing on screen explaining why — so the selector is on the page rather
 * than behind an expander.
 *
 * **A harness must not be called broken.** `ready: false` also covers one the controller
 * has not observed yet, which is a different thing from one that failed — and the
 * `kagent` harness on the development cluster is exactly that: it runs agents and carries
 * `status: null`. Calling that "broken" sends somebody debugging a harness that works.
 *
 * ## Why the form is short
 *
 * The CRD is strict, and the constraints the form enforces are the cluster's rather than
 * this page's: exactly one runtime adapter, an image pinned by digest, and a worker pool
 * for the Substrate Actors to be scheduled onto. A form that accepted a tag would build a
 * resource the cluster rejects — the failure that is invisible until somebody tries it
 * for real, which is why the fixture refuses it too.
 */

/** The one this journey makes, reads back and removes. */
const CREATED = "made-here";

/** Its rows, which is the surface that can say whether any of this happened. */
const table = "harnesses-table";

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("harnesses: a harness is created, read and deleted", async ({ page }) => {
  await test.step("1. the harnesses are listed, with what admits a template on the page", async () => {
    await loadPage(page, routes.harnesses, { title: "Agents" });
    await expect(page.getByTestId(table)).toBeVisible({ timeout: 30_000 });
    await expect(page.locator("tbody tr").first()).toBeVisible();

    // The whole reason a template does or does not become an agent, so it is read
    // without expanding a row.
    await expect(page.getByTestId("harness-selector").first()).toBeVisible();
  });

  await test.step("2. an unobserved harness is 'not ready yet', never 'broken'", async () => {
    const states = await page.getByTestId("harness-ready").allTextContents();
    expect(states.length).toBeGreaterThan(0);
    for (const state of states) {
      expect(
        state.toLowerCase(),
        "a harness the controller has not observed is not a broken one",
      ).not.toContain("broken");
      expect(state).toMatch(/Ready|Not ready yet/);
    }
  });

  await test.step("3. the list narrows like every other table", async () => {
    await expect(page.getByTestId("harnesses-filters")).toContainText("All namespaces");
    const rows = page.getByTestId(table).locator("tbody tr");
    const before = await rows.count();

    await page
      .getByTestId("harnesses-filters")
      .getByRole("textbox")
      .fill("no-such-harness");
    await expect.poll(() => rows.count()).toBeLessThan(before);

    await page.getByTestId("harnesses-filters").getByRole("textbox").fill("");
    await expect.poll(() => rows.count()).toBe(before);
  });

  await test.step("4. a harness with no selector says it will run nothing", async () => {
    await page.getByTestId("agents-new-harness").click();
    await page.waitForURL(/\/harnesses\/new(\?|$)/);

    // Legal, and almost never intended: the CRD admits no templates when the selector is
    // omitted, so the harness is created and does nothing with no sign of why.
    await expect(page.getByTestId("harness-admits-nothing")).toBeVisible();

    /*
     * And the marks agree with what the form will refuse. antd draws the asterisk from
     * `required` on a `Form.Item` while this form gates its submit in code, so the mark
     * and the gate are two separate statements about the same field with nothing but
     * this keeping them agreeing.
     *
     * The selector is deliberately unmarked: it is optional in the CRD's sense, and the
     * warning above says what omitting it costs. A mark there would refuse a harness the
     * cluster accepts.
     */
    await expectRequired(page, {
      marked: [
        "Namespace",
        "Name",
        "Runtime adapter",
        "Workload image",
        "Worker pool",
        "Snapshot location",
      ],
      unmarked: ["Admits agent templates labelled"],
    });
  });

  await test.step("5. an image that is not pinned cannot be submitted", async () => {
    await page.getByTestId("harness-namespace").click();
    await page.locator(".ant-select-item-option").first().click();
    await page.getByTestId("harness-name").fill(CREATED);
    await page.getByTestId("harness-worker-pool").fill("kagent-default");

    await page.getByTestId("harness-image").fill("ghcr.io/example/runtime:latest");
    await expect(
      page.getByTestId("harness-create"),
      "a tag can move under a running agent, and the CRD refuses one",
    ).toBeDisabled();
  });

  await test.step("6. nor can one with no snapshot location", async () => {
    // The CRD requires it. This form used to treat it as optional, so a harness could be
    // submitted without one and the controller answered "Invalid Harness" — naming
    // neither the field nor what was wrong with it.
    await page
      .getByTestId("harness-image")
      .fill(`ghcr.io/example/runtime@sha256:${"a".repeat(64)}`);
    await expect(page.getByTestId("harness-create")).toBeDisabled();
  });

  await test.step("7. pinned by digest and told where snapshots go, it is created", async () => {
    await page.getByTestId("harness-snapshot").fill("s3://ate-snapshots/kagent");
    await page.getByTestId("harness-selector-key").fill("runtime");
    await page.getByTestId("harness-selector-value").fill(CREATED);
    await expect(page.getByTestId("harness-admits-nothing")).toHaveCount(0);

    await expect(page.getByTestId("harness-create")).toBeEnabled();
    await page.getByTestId("harness-create").click();

    // Back to the tab it came from, with the new harness in the list. Read back off the
    // table rather than from a toast or a closed form: "the create returned" and "the
    // thing exists" are different claims, and only the list checks the second.
    await page.waitForURL(/tab=harnesses/);
    await expect(page.getByTestId(table)).toContainText(CREATED, { timeout: 30_000 });
  });

  await test.step("8. and it is not ready yet, which is what a cluster reports", async () => {
    // The controller has not observed it. A fixture that answered "ready" would hide the
    // one state a newly created harness is actually in.
    const row = page.getByTestId(table).locator("tr", { hasText: CREATED });
    await expect(row.getByTestId("harness-ready")).toContainText("Not ready yet");
  });

  await test.step("9. it is removed from the same tab, and the rest stays", async () => {
    const rows = page.getByTestId(table).locator("tbody tr");
    const before = await rows.count();

    await confirmDelete(page, CREATED);

    await expect(page.getByTestId(table)).not.toContainText(CREATED, {
      timeout: 30_000,
    });
    // One row went, not the table: "gone" has to mean that harness rather than a read
    // that failed and left an empty list behind it.
    await expect.poll(() => rows.count()).toBe(before - 1);
    await expect(page.getByTestId("harnesses-delete-error")).toHaveCount(0);
  });
});
