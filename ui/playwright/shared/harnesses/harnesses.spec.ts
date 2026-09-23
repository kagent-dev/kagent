import { test, expect } from "../../fixtures/test";
import {
  READ_TIMEOUT,
  expectListTotal,
  loadApp,
  readListTotal,
  throwawayName,
} from "../../helpers/app";
import { sweepQuietly } from "../../helpers/cleanup";
import {
  LIFECYCLE_TIMEOUT,
  appeared,
  confirmDelete,
  selectOption,
} from "../../helpers/resource";

/**
 * A harness created, read back and deleted — on either backend.
 *
 * There is no update half: the tab offers create and delete and no edit.
 *
 * The claim worth running against a cluster is step 3. A newly created harness is
 * "not ready yet" rather than broken — `ready: false` also covers one the controller
 * has not observed — and that is a state only a real controller genuinely produces.
 * A fixture answering "ready" would hide the one state a new harness is actually in.
 */

const CREATED = throwawayName("harness");

/** A draft the cluster would accept, so that each refusal below is about one field. */
const PINNED = `ghcr.io/example/runtime@sha256:${"a".repeat(64)}`;
const SNAPSHOT = "s3://ate-snapshots/kagent";

/** Its rows, which is the surface that can say whether any of this happened. */
const table = "harnesses-table";

test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

/** Whether this run has a harness on the cluster, read by the hook below. */
let created = false;

/*
 * Cleanup in a hook, not in the body's `finally`.
 *
 * A timed-out test is the likeliest live failure — a controller that never reconciles —
 * and it is exactly the one a `finally` cannot clean up after: Playwright has closed the
 * page by then, so every call in it throws. Measured, with a four-second test: the
 * `finally` was refused with "Target page, context or browser has been closed" while
 * this hook still drove the page. Hooks get their own budget, which is the point.
 */
test.afterEach(async ({ page }) => {
  if (!created) return;
  created = false;
  await sweepQuietly(CREATED, async () => {
    await loadApp(page, "/agents?tab=harnesses");
    // Waited for, not counted once: `loadApp` returns as soon as the shell is up, and a
    // tab still fetching has no rows — which reads as "already gone" and leaves a real
    // Harness on the cluster. See `appeared`.
    if (await appeared(page.getByTestId(table).getByText(CREATED).first())) {
      await confirmDelete(page, CREATED);
    }
  });
});

test("harnesses: a harness is created, read and deleted", async ({ page }) => {
  /** What the tab held before this journey, so the counts below can be relative. */
  let before = 0;

  await test.step("1. each field the cluster would refuse, refused on its own", async () => {
    // Counted first: an absolute count is the fixtures' to make, but "one more, then
    // one fewer" holds on any cluster.
    await loadApp(page, "/agents?tab=harnesses");
    /*
     * Off the summary, like every other list here. Counting rows instead needed a row to
     * wait for, which demanded that the cluster already own a harness — and reading the
     * count straight after the summary does not work either: the summary renders a frame
     * before the rows do, so the count came back 0 against a tab holding four.
     */
    before = await readListTotal(page, "harnesses");

    await loadApp(page, "/harnesses/new");
    const create = page.getByTestId("harness-create");

    await selectOption(page, "harness-namespace", "kagent");
    await page.getByTestId("harness-name").fill(CREATED);
    await page.getByTestId("harness-worker-pool").fill("kagent-default");
    await page.getByTestId("harness-image").fill(PINNED);
    await page.getByTestId("harness-snapshot").fill(SNAPSHOT);

    // A harness with no selector admits nothing, and the form says so before it is
    // asked to create one — the one state that is a warning rather than a refusal.
    await expect(page.getByTestId("harness-admits-nothing")).toBeVisible();
    await page.getByTestId("harness-selector-key").fill("runtime");
    await page.getByTestId("harness-selector-value").fill(CREATED);
    await expect(page.getByTestId("harness-admits-nothing")).toHaveCount(0);

    /*
     * Enabled with a complete draft, and each field below then broken on its own.
     * Asserted the other way round — a bad image on a half-filled form — both of
     * these passed on a form that had never looked at the field in question: the
     * empty snapshot location was disabling the button by itself.
     */
    await expect(create).toBeEnabled();

    // The image, which the cluster refuses as a tag because a tag can move under a
    // running agent. The form says which of the two it is unhappy about.
    await page.getByTestId("harness-image").fill("ghcr.io/example/runtime:latest");
    await expect(create).toBeDisabled();
    await expect(page.getByText(/Pin the image by digest/)).toBeVisible();
    await page.getByTestId("harness-image").fill(PINNED);
    await expect(create).toBeEnabled();

    // And the snapshot location, which the CRD requires and which the controller
    // would otherwise reject as "Invalid Harness", naming no field.
    await page.getByTestId("harness-snapshot").fill("");
    await expect(create).toBeDisabled();
    await page.getByTestId("harness-snapshot").fill(SNAPSHOT);
  });

  await test.step("2. a complete draft is created", async () => {
    await expect(page.getByTestId("harness-create")).toBeEnabled();
    // Set before the submit, not after the redirect: a create the controller accepted
    // but whose redirect was slow would otherwise fail the test with the flag still
    // false, and the cleanup would skip a resource that really is on the cluster. The
    // sweep looks for the row, so claiming one that was never made costs nothing.
    created = true;
    await page.getByTestId("harness-create").click();

    // Back to the tab it came from, with the new harness in the list. Read back off
    // the table rather than from a toast: "the create returned" and "the thing
    // exists" are different claims, and only the list checks the second.
    await page.waitForURL(/tab=harnesses/, { timeout: READ_TIMEOUT });
    await expect(page.getByTestId(table)).toContainText(CREATED, {
      timeout: READ_TIMEOUT,
    });
    await expectListTotal(page, "harnesses", before + 1);
  });

  await test.step("3. and it is not ready yet, which is what a cluster reports", async () => {
    const row = page.getByTestId(table).locator("tr", { hasText: CREATED });
    await expect(row.getByTestId("harness-ready")).toContainText("Not ready yet", {
      timeout: READ_TIMEOUT,
    });
  });

  await test.step("4. it is removed from the same tab, and the rest stays", async () => {
    await confirmDelete(page, CREATED);

    await expect(page.getByTestId(table)).not.toContainText(CREATED, {
      timeout: READ_TIMEOUT,
    });
    created = false;
    // One row went, not the table: "gone" has to mean that harness rather than a read
    // that failed and left an empty list behind it, and the summary is drawn only for a
    // read that succeeded.
    await expectListTotal(page, "harnesses", before);
    await expect(page.getByTestId("harnesses-delete-error")).toHaveCount(0);
  });
});
