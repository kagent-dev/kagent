import { test, expect } from "../../fixtures/test";
import { loadPage, routes } from "../../helpers/app";

/**
 * The harnesses tab, which replaced the create-an-agent form.
 *
 * That form is gone because what it created does not exist: there is no Agent CRD, and
 * an agent is what you get once a harness admits a template. But the form carried two
 * things a reader still needs, and they moved here rather than being lost with it.
 *
 * **The admission selector has to be visible.** A harness admits templates through a
 * label selector, and that selector is what decides whether a template ever becomes an
 * agent at all. A template carrying no label it matches saves happily and then does
 * nothing, with nothing on screen explaining why — so the selector is on the page
 * rather than behind an expander.
 *
 * **A harness must not be called broken.** `ready: false` also covers one the
 * controller has not observed yet, which is a different thing from one that failed —
 * and the `kagent` harness on the development cluster is exactly that: it runs agents
 * and carries `status: null`. Calling that "broken" sends somebody debugging a harness
 * that works.
 */
test("harnesses: the tab says what admits a template, and does not call a new harness broken", async ({
  page,
}) => {
  await loadPage(page, routes.harnesses, { title: "Agents" });

  await test.step("1. the harnesses are listed", async () => {
    await expect(page.getByTestId("harnesses-table")).toBeVisible({ timeout: 30_000 });
    await expect(page.locator("tbody tr").first()).toBeVisible();
  });

  await test.step("2. the admission selector is on the page, not behind anything", async () => {
    // The whole reason a template does or does not become an agent, so it is read
    // without expanding a row.
    await expect(page.getByTestId("harness-selector").first()).toBeVisible();
  });

  await test.step("3. an unobserved harness is 'not ready yet', never 'broken'", async () => {
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

  await test.step("4. a harness can be made and removed from here", async () => {
    /*
     * This tab was read-only, on a note in the codebase saying `HarnessService` was
     * read-only in this build. It was not: the service implements create, update and
     * delete and always did — what was read-only was the application, which only ever
     * called `list`.
     */
    await expect(page.getByTestId("agents-new-harness")).toBeVisible();
    await expect(
      page.getByTestId("harnesses-table").locator('[data-testid^="delete-"]').first(),
      "each harness offers a delete, because one can be removed",
    ).toBeVisible();
  });

  await test.step("5. the list narrows like every other table", async () => {
    await expect(page.getByTestId("harnesses-filters")).toContainText("All namespaces");
    const before = await page.getByTestId("harnesses-table").locator("tbody tr").count();
    await page.getByTestId("harnesses-filters").getByRole("textbox").fill("no-such-harness");
    await expect
      .poll(() => page.getByTestId("harnesses-table").locator("tbody tr").count())
      .toBeLessThan(before);
  });
});

/**
 * Creating a harness, and being refused the two ways a cluster would refuse it.
 *
 * The form is short because the CRD is strict, and the constraints it enforces are the
 * cluster's rather than this page's: exactly one runtime adapter, an image pinned by
 * digest, and a worker pool for the Substrate Actors to be scheduled onto. A form that
 * accepted a tag would build a resource the cluster rejects — the failure that is
 * invisible until somebody tries it for real, which is why the fixture refuses it too.
 */
test("harnesses: one can be created, and a tag is refused the way a cluster refuses it", async ({
  page,
}) => {
  await loadPage(page, routes.harnessNew, { title: "New harness" });

  await test.step("1. a harness with no selector says it will run nothing", async () => {
    // Legal, and almost never intended: the CRD admits no templates when the selector
    // is omitted, so the harness is created and does nothing with no sign of why.
    await expect(page.getByTestId("harness-admits-nothing")).toBeVisible();
  });

  await test.step("2. an image that is not pinned cannot be submitted", async () => {
    await page.getByTestId("harness-namespace").click();
    await page.locator(".ant-select-item-option").first().click();
    await page.getByTestId("harness-name").fill("made-here");
    await page.getByTestId("harness-worker-pool").fill("kagent-default");

    await page.getByTestId("harness-image").fill("ghcr.io/example/runtime:latest");
    await expect(
      page.getByTestId("harness-create"),
      "a tag can move under a running agent, and the CRD refuses one",
    ).toBeDisabled();
  });

  await test.step("3. nor can one with no snapshot location", async () => {
    // The CRD requires it. This form used to treat it as optional, so a harness
    // could be submitted without one and the controller answered "Invalid Harness"
    // -- naming neither the field nor what was wrong with it.
    await page
      .getByTestId("harness-image")
      .fill(`ghcr.io/example/runtime@sha256:${"a".repeat(64)}`);
    await expect(page.getByTestId("harness-create")).toBeDisabled();
  });

  await test.step("4. pinned by digest and told where snapshots go, it can be created", async () => {
    await page.getByTestId("harness-snapshot").fill("s3://ate-snapshots/kagent");
    await page.getByTestId("harness-selector-key").fill("runtime");
    await page.getByTestId("harness-selector-value").fill("made-here");
    await expect(page.getByTestId("harness-admits-nothing")).toHaveCount(0);

    await expect(page.getByTestId("harness-create")).toBeEnabled();
    await page.getByTestId("harness-create").click();

    // Back to the tab it came from, with the new harness in the list.
    await page.waitForURL(/tab=harnesses/);
    await expect(page.getByTestId("harnesses-table")).toContainText("made-here", {
      timeout: 30_000,
    });
  });

  await test.step("5. and it is not ready yet, which is what a cluster reports", async () => {
    // The controller has not observed it. A fixture that answered "ready" would hide
    // the one state a newly created harness is actually in.
    const row = page.getByTestId("harnesses-table").locator("tr", { hasText: "made-here" });
    await expect(row.getByTestId("harness-ready")).toContainText("Not ready yet");
  });
});
