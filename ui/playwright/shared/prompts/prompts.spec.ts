import { type Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import {
  READ_TIMEOUT,
  expectListLoaded,
  expectListTotal,
  expectNoLoadFailure,
  loadApp,
  readListTotal,
  rowNamed,
  searchList,
  throwawayName,
} from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  confirmDelete,
} from "../../helpers/resource";
import { sweepUp } from "../../helpers/cleanup";

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

/** Whether this run has a library on the cluster, read by the hook below. */
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
  await sweepUp(page, "prompts", CREATED);
});

test("prompts: a library is created, read, changed and deleted", async ({ page }) => {
  /** What the list held before this journey, so the counts below can be relative. */
  let before = 0;

  await test.step("1. a library with one fragment is created and listed", async () => {
    // Counted first, and relative from here on: the fixtures seed two and a cluster
    // seeds whatever it was installed with, but "one more than before" is exactly as
    // strong and catches a create that wrote twice. Off the summary rather than by
    // counting rows, the table paging at 25 — see `readListTotal`.
    await loadApp(page, "/prompts");
    // Off the summary alone — see `models`: waiting for a row demanded a library the
    // cluster need not have.
    before = await readListTotal(page, "prompts");

    await page.getByTestId("prompts-new").click();
    await expect(page.getByTestId("prompt-submit")).toBeVisible({ timeout: READ_TIMEOUT });

    await page.getByTestId("prompt-name").fill(CREATED);
    await page.getByTestId("prompt-namespace").fill("kagent");
    await fragmentKey(page, 0).fill("changelog");
    await fragmentValue(page, 0).fill("Group by user impact.");

    await page.getByTestId("prompt-submit").click();
    await expect(page).toHaveURL(/\/prompts$/, { timeout: READ_TIMEOUT });
    created = true;

    // Read back off the list rather than from a toast or a closed form: those two
    // only prove the app believes it worked. Narrowed to the one name this run
    // invented, so the assertions below are about that row wherever the cluster's own
    // libraries put it.
    await searchList(page, "prompts", CREATED);
    // The list has answered before its alerts are counted — see `expectNoLoadFailure`.
    await expectListLoaded(page, "prompts");
    await expectNoLoadFailure(page);
    const row = rowNamed(page, CREATED);
    await expect(row).toContainText("1 key", { timeout: READ_TIMEOUT });
    await expect(row).toContainText("changelog");
    await expectListTotal(page, "prompts", before + 1);
  });

  await test.step("2. opening it shows the fragment and how to include it", async () => {
    await rowNamed(page, CREATED).getByRole("link").first().click();
    await page.waitForURL(new RegExp(`/prompts/kagent/${CREATED}$`), {
      timeout: READ_TIMEOUT,
    });

    const fragments = page.getByTestId("prompt-fragments");
    await expect(fragments).toContainText("changelog", { timeout: READ_TIMEOUT });
    await expect(fragments).toContainText("Group by user impact.");
  });

  await test.step("3. a fragment is added, saved, and read back off the library", async () => {
    await page.getByTestId("prompt-edit").click();
    await page.waitForURL(new RegExp(`/prompts/kagent/${CREATED}/edit$`), {
      timeout: READ_TIMEOUT,
    });
    // Seeded from the saved library, so the form and the page it came from agree.
    await expect(fragmentKey(page, 0)).toHaveValue("changelog", { timeout: READ_TIMEOUT });

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
      timeout: READ_TIMEOUT,
    });

    // Read back from the re-read library rather than from the draft: a save that
    // never reached the backend would leave the old text here.
    const fragments = page.getByTestId("prompt-fragments");
    await expect(fragments).toContainText("Name the next owner explicitly.", {
      timeout: READ_TIMEOUT,
    });
    await expect(page.getByTestId("prompt-detail-meta")).toContainText("2 fragments");
  });

  await test.step("4. the list behind it shows the change too", async () => {
    await page.getByRole("link", { name: "Back to libraries" }).click();
    // The search went with the detail page; the list is whole again on the way back.
    await searchList(page, "prompts", CREATED);
    await expect(rowNamed(page, CREATED)).toContainText("2 keys", { timeout: READ_TIMEOUT });
    await expect(rowNamed(page, CREATED)).toContainText("handoff");
  });

  await test.step("5. confirming a delete removes that row and leaves the rest", async () => {
    await confirmDelete(page, CREATED);
    await expect(rowNamed(page, CREATED)).toHaveCount(0, { timeout: READ_TIMEOUT });
    created = false;

    // One row went, not several, and not the read: a list that failed to reload is
    // also a list the row is missing from, and the summary the total is read off
    // renders only for a load that succeeded — so it is what is waited for, and the
    // alert count after it is what names a failure.
    await expectListTotal(page, "prompts", before);
    await expectNoLoadFailure(page);
  });
});
