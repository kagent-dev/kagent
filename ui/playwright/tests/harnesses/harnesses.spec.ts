import { test, expect } from "../../fixtures/test";
import { loadPage, routes } from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  expectRequired,
  selectFirstOption,
} from "../../helpers/resource";

/**
 * Harnesses, on the fixtures. The tab offers create and delete and no edit, and that
 * journey runs against both backends from `shared/harnesses/`. What stays is the reading
 * — the seeded rows, the selector on the page, the narrowing — and the two refusals
 * below, which are the form enforcing the cluster's constraints rather than creating
 * anything.
 *
 * **The admission selector has to be visible.** A harness admits templates through a
 * label selector, and that selector decides whether a template ever becomes an agent at
 * all. One carrying no label it matches saves happily and then does nothing, with nothing
 * on screen explaining why.
 *
 * **A harness must not be called broken.** `ready: false` also covers one the controller
 * has not observed yet, which is a different thing from one that failed — the `kagent`
 * harness on a development cluster runs agents and carries `status: null`.
 *
 * **The form is short because the CRD is strict**: exactly one runtime adapter, an image
 * pinned by digest, and a worker pool to schedule onto. A form that accepted a tag would
 * build a resource the cluster rejects, which is why the fixture refuses it too.
 */

/** The name the validation steps type in. Nothing is created here — see the note above. */
const CREATED = "made-here";

/** Its rows, which is the surface that can say whether any of this happened. */
const table = "harnesses-table";

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("harnesses: the tab reads, and the form refuses what the CRD refuses", async ({
  page,
}) => {
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
    // Any namespace will do: this step is about the image, not about where the harness
    // lives.
    await selectFirstOption(page, "harness-namespace");
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

  await test.step("7. an empty result leaves the tab standing, with no rows", async () => {
    // Last, after the delete: reaching these needs the backend answering differently and
    // `?mock=` is per-navigation, which discards what the journey made.
    await loadPage(page, routes.harnesses, { scenario: "empty", title: "Agents" });
    await expect(page.getByTestId(table)).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId(table).locator("tbody tr.ant-table-row")).toHaveCount(0);
    await expect(page.getByTestId("harnesses-error")).toHaveCount(0);
  });

  await test.step("8. a failed load is reported, not disguised as an empty tab", async () => {
    await loadPage(page, routes.harnesses, { scenario: "error", title: "Agents" });

    const alert = page.getByTestId("harnesses-error");
    await expect(alert).toBeVisible({ timeout: 30_000 });
    await expect(alert).toContainText("Could not load harnesses");
    // An empty tab and a tab that could not be read lead to opposite conclusions, and
    // this page has only the alert to tell them apart.
    await expect(page.getByTestId(table).locator("tbody tr.ant-table-row")).toHaveCount(0);
  });
});
