import { type Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { loadApp, throwawayName } from "../../helpers/app";
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

/** Its rows, which is the surface that can say whether any of this happened. */
const table = "harnesses-table";

/*
 * Data rows only. antd draws its loading and empty placeholders as a `tbody tr` too, so
 * a bare `tbody tr` counts one row for a table that has not loaded — which read as a
 * seeded set of one here, and only failed because the count afterwards disagreed.
 */
const harnessRows = (page: Page) =>
  page.getByTestId(table).locator("tbody tr.ant-table-row");

test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("harnesses: a harness is created, read and deleted", async ({ page }) => {
  let created = false;
  /** What the tab held before this journey, so the counts below can be relative. */
  let before = 0;

  try {
    await test.step("1. the form refuses an image that is not pinned", async () => {
      // Counted first: an absolute count is the fixtures' to make, but "one more, then
      // one fewer" holds on any cluster.
      await loadApp(page, "/agents?tab=harnesses");
      await expect(harnessRows(page).first()).toBeVisible({ timeout: 60_000 });
      before = await harnessRows(page).count();

      await loadApp(page, "/harnesses/new");

      await selectOption(page, "harness-namespace", "kagent");
      await page.getByTestId("harness-name").fill(CREATED);
      await page.getByTestId("harness-worker-pool").fill("kagent-default");

      // The constraints here are the cluster's rather than this page's: a tag can move
      // under a running agent, and the CRD refuses one. A form that accepted it would
      // build a resource the cluster rejects.
      await page.getByTestId("harness-image").fill("ghcr.io/example/runtime:latest");
      await expect(page.getByTestId("harness-create")).toBeDisabled();

      // The CRD requires a snapshot location too, so it is still refused without one.
      await page
        .getByTestId("harness-image")
        .fill(`ghcr.io/example/runtime@sha256:${"a".repeat(64)}`);
      await expect(page.getByTestId("harness-create")).toBeDisabled();
    });

    await test.step("2. pinned by digest and told where snapshots go, it is created", async () => {
      await page.getByTestId("harness-snapshot").fill("s3://ate-snapshots/kagent");
      await page.getByTestId("harness-selector-key").fill("runtime");
      await page.getByTestId("harness-selector-value").fill(CREATED);
      await expect(page.getByTestId("harness-admits-nothing")).toHaveCount(0);

      await expect(page.getByTestId("harness-create")).toBeEnabled();
      await page.getByTestId("harness-create").click();

      // Back to the tab it came from, with the new harness in the list. Read back off
      // the table rather than from a toast: "the create returned" and "the thing
      // exists" are different claims, and only the list checks the second.
      await page.waitForURL(/tab=harnesses/, { timeout: 60_000 });
      created = true;
      await expect(page.getByTestId(table)).toContainText(CREATED, { timeout: 60_000 });
      await expect.poll(() => harnessRows(page).count(), { timeout: 60_000 }).toBe(
        before + 1,
      );
    });

    await test.step("3. and it is not ready yet, which is what a cluster reports", async () => {
      const row = page.getByTestId(table).locator("tr", { hasText: CREATED });
      await expect(row.getByTestId("harness-ready")).toContainText("Not ready yet", {
        timeout: 60_000,
      });
    });

    await test.step("4. it is removed from the same tab, and the rest stays", async () => {
      await confirmDelete(page, CREATED);

      await expect(page.getByTestId(table)).not.toContainText(CREATED, {
        timeout: 60_000,
      });
      created = false;
      // One row went, not the table: "gone" has to mean that harness rather than a read
      // that failed and left an empty list behind it.
      await expect.poll(() => harnessRows(page).count(), { timeout: 60_000 }).toBe(before);
      await expect(page.getByTestId("harnesses-delete-error")).toHaveCount(0);
    });
  } finally {
    if (created) {
      await loadApp(page, "/agents?tab=harnesses");
      // Waited for, not counted once: `loadApp` returns as soon as the shell is up, and
      // a tab still fetching has no rows — which reads as "already gone" and leaves a
      // real Harness on the cluster. See `appeared`.
      if (await appeared(page.getByTestId(table).getByText(CREATED).first())) {
        await confirmDelete(page, CREATED);
      }
    }
  }
});
