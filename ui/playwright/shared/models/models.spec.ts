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
import {
  LIFECYCLE_TIMEOUT,
  confirmDelete,
  confirmation,
  selectOption,
} from "../../helpers/resource";
import { sweepUp } from "../../helpers/cleanup";

/**
 * A model configuration created, read back, changed and deleted — on either backend.
 *
 * The write path is where a fixture and a controller most easily disagree: a create
 * wrapped one way in the fixtures and another by the API, a name sent where a ref
 * belonged. The fixtures answer whatever they were taught, so only a cluster can
 * settle it — and only this spec asks both the same question.
 *
 * What stays in `tests/models/models.spec.ts` is what needs the fixtures: the exact
 * seeded rows, the required-field marks, the refresh count, and the empty and failure
 * states that no cluster can be asked for.
 */

/** The one this journey makes, changes and removes. Unique, so a killed run leaves litter a person can spot. */
const CREATED = throwawayName("model");
const SECRET = "kagent-shared-e2e-secret";

test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("models: a configuration is created, read, changed and deleted", async ({
  page,
}) => {
  let created = false;
  /** What the list held before this journey, so the counts below can be relative. */
  let before = 0;

  try {
    await test.step("1. a filled-in configuration is created and appears on the list", async () => {
      /*
       * Counted first, and relative from here on. The fixtures seed four and a cluster
       * seeds whatever it was installed with, so an absolute count is the one thing this
       * spec cannot assert — but "one more than before" is exactly as strong, and it is
       * what catches a create that wrote two rows or a delete that took a neighbour.
       *
       * Off the summary rather than by counting rows: the table pages at 25, and a
       * cluster is free to hold more than that — see `readListTotal`.
       */
      await loadApp(page, "/models");
      await expect(dataRows(page).first()).toBeVisible({ timeout: 60_000 });
      before = await readListTotal(page, "models");

      await page.getByTestId("models-new").click();
      await page.waitForURL(/\/models\/new(\?|$)/, { timeout: 60_000 });

      // The provider list is the app's own enum rather than the backend's, so the name
      // a reader sees is the same on either — `providerDisplayName` turns
      // `AmazonBedrock` into "AWS Bedrock".
      await selectOption(page, "model-provider", "Anthropic");

      // An AutoComplete, not a Select: the id is on the wrapper and the caret goes in
      // the input inside it. Typed rather than picked, the field existing to accept a
      // model the catalogue has not heard of.
      await page.getByTestId("model-model").locator("input").fill("claude-sonnet-4");

      await page.getByTestId("model-name").fill(CREATED);
      // The namespace is half the ref, so one created without it is addressed as
      // `/name` and never appears on the list. `kagent` is where both backends put
      // things.
      await selectOption(page, "model-namespace", "kagent");
      await page.getByTestId("model-api-key").fill("sk-not-a-real-key");

      await page.getByTestId("model-submit").click();
      await page.waitForURL(/\/models(\?|$)/, { timeout: 60_000 });
      created = true;

      // Read back off the list rather than from a toast or a closed form: those two
      // only prove the app believes it worked. Narrowed to the one name this run
      // invented, so the assertions below are about that row wherever the cluster's own
      // configurations put it.
      await searchList(page, "models", CREATED);
      // The list has answered before its alerts are counted: `waitForURL` lands on a
      // page that has not read anything yet, where there is nothing to count.
      await expectListLoaded(page, "models");
      await expectNoLoadFailure(page);
      const row = rowNamed(page, CREATED);
      await expect(row).toHaveCount(1, { timeout: 60_000 });
      await expect(row).toContainText("Anthropic");
      await expect(row).toContainText("claude-sonnet-4");
      await expectListTotal(page, "models", before + 1);
    });

    await test.step("2. the edit form opens on what was saved, not a blank draft", async () => {
      await page.getByTestId(`edit-${CREATED}`).click();
      await page.waitForURL(new RegExp(`/models/kagent/${CREATED}/edit$`), {
        timeout: 60_000,
      });

      await expect(page.getByTestId("model-name")).toHaveValue(CREATED, {
        timeout: 60_000,
      });
      /*
       * The identity and the provider are the ref and what the ref means, so an edit
       * changes neither. Asserted here because it is the boundary between "edit" and
       * "make a new one", and a form that quietly allowed it would write a resource
       * nothing else in the cluster points at.
       */
      await expect(page.getByTestId("model-name")).toBeDisabled();
      await expect(page.getByTestId("model-model").locator("input")).toBeDisabled();
    });

    await test.step("3. a change is saved, and the list shows it", async () => {
      // The credential moves from one the backend minted to a secret it is told to
      // read — the one change on this form the list has a column for, which is what
      // makes the save checkable from outside the form.
      await page
        .getByTestId("model-auth-type")
        .getByText("Existing secret", { exact: true })
        .click();
      await page.getByTestId("model-api-key-secret").fill(SECRET);

      await page.getByTestId("model-submit").click();
      await page.waitForURL(/\/models(\?|$)/, { timeout: 60_000 });

      // The search went with the form; the list is whole again on the way back.
      await searchList(page, "models", CREATED);
      const row = rowNamed(page, CREATED);
      await expect(row).toContainText(SECRET, { timeout: 60_000 });
      // Changed, not duplicated — which a create dressed as an update would be.
      await expect(row).toHaveCount(1);
      await expectListTotal(page, "models", before + 1);
    });

    await test.step("4. deleting asks first, and Keep leaves it alone", async () => {
      await page.getByTestId(`delete-${CREATED}`).click();
      const prompt = confirmation(page);
      // The confirmation names the row. "Delete this model configuration?" is no help
      // in a table of five, and *which* is the one question the reader has.
      await expect(prompt).toContainText(CREATED);
      await prompt.getByRole("button", { name: "Keep" }).click();
      await expect(rowNamed(page, CREATED)).toHaveCount(1);
      // Waited out rather than assumed gone: the dialog stays visible while it
      // animates away, and the next step's click would land on it.
      await expect(prompt).toHaveCount(0);
    });

    await test.step("5. confirming removes that row and leaves the rest", async () => {
      await confirmDelete(page, CREATED);
      await expect(rowNamed(page, CREATED)).toHaveCount(0, { timeout: 60_000 });
      created = false;

      // One row went, not several, and not the read: a list that failed to reload is
      // also a list the row is missing from, and the summary the total is read off
      // renders only for a load that succeeded — so it is what is waited for, and the
      // alert count after it is what names a failure.
      await expectListTotal(page, "models", before);
      await expectNoLoadFailure(page);
    });
  } finally {
    // A real resource on a real cluster when this runs live, so a run that dies midway
    // takes it with it.
    if (created) await sweepUp(page, "models", CREATED);
  }
});
