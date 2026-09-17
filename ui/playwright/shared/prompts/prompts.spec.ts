import { type Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import {
  dataRows,
  expectListLoaded,
  expectListTotal,
  expectNoLoadFailure,
  loadApp,
  readListTotal,
  rowNamed,
  searchList,
  throwawayName,
} from "../../helpers/app";
import { LIFECYCLE_TIMEOUT, confirmDelete } from "../../helpers/resource";

/**
 * A prompt library created, read, changed and deleted — on either backend.
 *
 * A library is fragments keyed by name, and the write path is where a fixture and a
 * controller most easily disagree about the shape of one. What stays in
 * `tests/prompts/prompts.spec.ts` is the seeded libraries, the namespace filter, the
 * discard prompts, and the empty and failure states.
 */

const CREATED = throwawayName("library");

/** The nth fragment row's key and text boxes. */
const fragmentKey = (page: Page, index: number) =>
  page.getByTestId("fragment-key").nth(index);
const fragmentValue = (page: Page, index: number) =>
  page.getByTestId("fragment-value").nth(index);

test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("prompts: a library is created, read, changed and deleted", async ({ page }) => {
  let created = false;
  /** What the list held before this journey, so the counts below can be relative. */
  let before = 0;

  try {
    await test.step("1. a library with one fragment is created and listed", async () => {
      // Counted first, and relative from here on: the fixtures seed two and a cluster
      // seeds whatever it was installed with, but "one more than before" is exactly as
      // strong and catches a create that wrote twice. Off the summary rather than by
      // counting rows, the table paging at 25 — see `readListTotal`.
      await loadApp(page, "/prompts");
      await expect(dataRows(page).first()).toBeVisible({ timeout: 60_000 });
      before = await readListTotal(page, "prompts");

      await page.getByTestId("prompts-new").click();
      await expect(page.getByTestId("prompt-submit")).toBeVisible({ timeout: 60_000 });

      await page.getByTestId("prompt-name").fill(CREATED);
      await page.getByTestId("prompt-namespace").fill("kagent");
      await fragmentKey(page, 0).fill("changelog");
      await fragmentValue(page, 0).fill("Group by user impact.");

      await page.getByTestId("prompt-submit").click();
      await expect(page).toHaveURL(/\/prompts$/, { timeout: 60_000 });
      created = true;

      // Read back off the list rather than from a toast or a closed form: those two
      // only prove the app believes it worked.
      await expectNoLoadFailure(page);
      // Narrowed to the one name this run invented, so the assertions below are about
      // that row wherever the cluster's own libraries put it.
      await searchList(page, "prompts", CREATED);
      const row = rowNamed(page, CREATED);
      await expect(row).toContainText("1 key", { timeout: 60_000 });
      await expect(row).toContainText("changelog");
      await expectListTotal(page, "prompts", before + 1);
    });

    await test.step("2. opening it shows the fragment and how to include it", async () => {
      await rowNamed(page, CREATED).getByRole("link").first().click();
      await page.waitForURL(new RegExp(`/prompts/kagent/${CREATED}$`), {
        timeout: 60_000,
      });

      const fragments = page.getByTestId("prompt-fragments");
      await expect(fragments).toContainText("changelog", { timeout: 60_000 });
      await expect(fragments).toContainText("Group by user impact.");
    });

    await test.step("3. a fragment is added, saved, and read back off the library", async () => {
      await page.getByTestId("prompt-edit").click();
      await page.waitForURL(new RegExp(`/prompts/kagent/${CREATED}/edit$`), {
        timeout: 60_000,
      });
      // Seeded from the saved library, so the form and the page it came from agree.
      await expect(fragmentKey(page, 0)).toHaveValue("changelog", { timeout: 60_000 });

      await page.getByTestId("fragment-add").click();
      await fragmentKey(page, 1).fill("handoff");
      await fragmentValue(page, 1).fill("Name the next owner explicitly.");
      // The include tag is what a fragment is for, and it is offered before the save
      // rather than only after it.
      await expect(page.getByTestId("fragment-include-preview").last()).toContainText(
        `{{include "${CREATED}/handoff"}}`,
      );

      await page.getByTestId("prompt-submit").click();
      await expect(page).toHaveURL(new RegExp(`/prompts/kagent/${CREATED}$`), {
        timeout: 60_000,
      });

      // Read back from the re-read library rather than from the draft: a save that
      // never reached the backend would leave the old text here.
      const fragments = page.getByTestId("prompt-fragments");
      await expect(fragments).toContainText("Name the next owner explicitly.", {
        timeout: 60_000,
      });
      await expect(page.getByTestId("prompt-detail-meta")).toContainText("2 fragments");
    });

    await test.step("4. the list behind it shows the change too", async () => {
      await page.getByRole("link", { name: "Back to libraries" }).click();
      // The search went with the detail page; the list is whole again on the way back.
      await searchList(page, "prompts", CREATED);
      await expect(rowNamed(page, CREATED)).toContainText("2 keys", { timeout: 60_000 });
      await expect(rowNamed(page, CREATED)).toContainText("handoff");
    });

    await test.step("5. confirming a delete removes that row and leaves the rest", async () => {
      await confirmDelete(page, CREATED);
      await expect(rowNamed(page, CREATED)).toHaveCount(0, { timeout: 60_000 });
      created = false;

      // One row went, not several, and not the read: a list that failed to reload is
      // also a list the row is missing from, and the summary the total is read off
      // renders only for a load that succeeded.
      await expectNoLoadFailure(page);
      await expectListTotal(page, "prompts", before);
    });
  } finally {
    if (created) {
      await loadApp(page, "/prompts");
      await searchList(page, "prompts", CREATED);
      // Counted only once the list has answered: a read still in flight has no rows
      // either, and taking that for "already gone" would leave it on the cluster.
      await expectListLoaded(page, "prompts");
      if ((await rowNamed(page, CREATED).count()) > 0) await confirmDelete(page, CREATED);
    }
  }
});
