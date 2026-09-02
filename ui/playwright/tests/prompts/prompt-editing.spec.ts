import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { dataRows, loadPage, rowNamed, routes } from "../../helpers/app";

/**
 * Editing a prompt library, from the action that used to go nowhere.
 *
 * The list's edit action landed on the read-only detail page: it promised an edit and
 * delivered the same place the library's name already opened, while
 * `UpdatePromptTemplate` — implemented end to end, from the RPC down to the ConfigMap
 * write — was reachable from nowhere in the app. So a library could be created and
 * read and then never changed.
 *
 * Driven as journeys rather than as page tests because the interesting parts are the
 * joins: that both edit actions reach the form, that a save replaces the library
 * rather than merging into it, and that the list behind it is re-read. The order the
 * rows arrive in is asserted too — `data` is a map on the wire, so the order is the
 * app's to impose, and the reading page and the form have to agree on it.
 */

/** `kagent/shared-fragments`, whose three keys sort into this order. */
const SEEDED_KEYS = ["escalation", "safety", "tone"];

/** The nth fragment row's key and text boxes. */
const fragmentKey = (page: Page, index: number) =>
  page.getByTestId("fragment-key").nth(index);
const fragmentValue = (page: Page, index: number) =>
  page.getByTestId("fragment-value").nth(index);

test("prompt libraries: the edit form opens from the list and from the library, and a save sticks", async ({
  page,
}) => {
  await test.step("1. the list's edit action opens the form, not the reading page", async () => {
    await loadPage(page, routes.prompts, { title: "Prompts" });
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(1, { timeout: 30_000 });

    await page.getByTestId("edit-shared-fragments").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments\/edit$/);
    await expect(page.getByTestId("prompt-submit")).toBeVisible();
  });

  await test.step("2. the rows are the library's fragments, in key order", async () => {
    await expect(page.getByTestId("fragment-row")).toHaveCount(SEEDED_KEYS.length);
    for (const [index, key] of SEEDED_KEYS.entries()) {
      await expect(fragmentKey(page, index)).toHaveValue(key);
    }
    await expect(fragmentValue(page, 2)).toHaveValue(/Be concise/);
  });

  await test.step("3. the form says a save replaces the library, because it does", async () => {
    // `UpdatePromptTemplate` assigns the ConfigMap's whole `data` map, so removing a
    // row deletes a fragment. Nothing else on screen would tell a reader that.
    await expect(page.getByTestId("prompt-fragments-note")).toContainText("is deleted");
    // And the identity is locked, because the ref addresses the ConfigMap: an edit
    // cannot rename a library or move it to another namespace.
    await expect(page.getByTestId("prompt-name")).toBeDisabled();
    await expect(page.getByTestId("prompt-namespace")).toBeDisabled();
  });

  await test.step("4. leaving with a draft asks before throwing it away", async () => {
    await fragmentValue(page, 2).fill("Edited by the suite.");
    await page.getByTestId("prompt-cancel").click();
    // The body rather than the modal: antd hangs the outer testid on a wrapper it
    // keeps hidden, so asserting on that one passes without the dialog being up.
    await expect(page.getByTestId("prompt-discard-body")).toBeVisible();

    await page.getByRole("button", { name: "Keep editing" }).click();
    // Kept, not lost — the point of asking. A prompt fragment is prose somebody
    // wrote, which is exactly the thing a silent discard costs most.
    await expect(fragmentValue(page, 2)).toHaveValue("Edited by the suite.");
  });

  await test.step("4b. and asks on every other way out, not just Cancel", async () => {
    /*
     * The header link and the sidebar, which is where a guard on the Cancel button
     * alone fails: it teaches a reader the work is held safe and then the two more
     * obvious exits throw it away without a word. Both are ordinary links, so what
     * is being asserted is that leaving is blocked rather than that a button asks.
     */
    for (const leave of [
      page.getByRole("link", { name: "Back to library" }),
      page.getByRole("link", { name: "Models" }),
    ]) {
      await leave.click();
      await expect(page.getByTestId("prompt-discard-body")).toBeVisible();
      await page.getByRole("button", { name: "Keep editing" }).click();
      await expect(page).toHaveURL(/\/edit$/);
      await expect(fragmentValue(page, 2)).toHaveValue("Edited by the suite.");
    }
  });

  await test.step("5. a fragment can be added alongside the edit", async () => {
    await page.getByTestId("fragment-add").click();
    await fragmentKey(page, 3).fill("handoff");
    await fragmentValue(page, 3).fill("Name the next owner explicitly.");
    // The include tag is what the fragment is for, and it is offered before the save
    // rather than only after it.
    await expect(page.getByTestId("fragment-include-preview").last()).toContainText(
      '{{include "shared-fragments/handoff"}}',
    );
  });

  await test.step("6. saving lands on the library, showing what was saved", async () => {
    // And is not itself treated as leaving with unsaved work: the draft stops being
    // unsaved before the caller navigates, so a save is never asked to confirm
    // itself. This URL assertion is what would fail if it were.
    await page.getByTestId("prompt-submit").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments$/, {
      timeout: 30_000,
    });

    const fragments = page.getByTestId("prompt-fragments");
    // Read back from the re-read library rather than from the draft: a save that
    // never reached the backend would leave the old text here.
    await expect(fragments).toContainText("Edited by the suite.");
    await expect(fragments).toContainText("Name the next owner explicitly.");
    await expect(page.getByTestId("prompt-detail-meta")).toContainText("4 fragments");
  });

  await test.step("7. the library's own Edit action reaches the same form", async () => {
    await page.getByTestId("prompt-edit").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments\/edit$/);
    // Seeded from the saved library, so the form and the page it came from agree.
    await expect(page.getByTestId("fragment-row")).toHaveCount(4);
    await expect(fragmentKey(page, 1)).toHaveValue("handoff");

    // And Cancel with nothing typed goes straight back, with nothing to confirm:
    // a question over a form nobody has touched is a question that teaches readers
    // to click through the next one.
    await page.getByTestId("prompt-cancel").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments$/);
    await expect(page.getByTestId("prompt-discard-body")).toHaveCount(0);
  });

  await test.step("8. the list behind it was re-read, not left stale", async () => {
    await page.getByRole("link", { name: "Back to libraries" }).click();
    const row = rowNamed(page, "shared-fragments");
    await expect(row).toContainText("4 keys", { timeout: 30_000 });
    // The keys column too, which is what the search on this page also covers.
    await expect(row).toContainText("handoff");
  });

  await test.step("9. and nothing else on the list moved", async () => {
    await expect(dataRows(page)).toHaveCount(2);
    await expect(rowNamed(page, "incident-playbooks")).toContainText("2 keys");
  });
});

test("prompt libraries: an edit that would empty a library is refused before it is sent", async ({
  page,
}) => {
  await loadPage(page, routes.prompts, { title: "Prompts" });
  await expect(rowNamed(page, "shared-fragments")).toHaveCount(1, { timeout: 30_000 });
  await page.getByTestId("edit-shared-fragments").click();
  await expect(page.getByTestId("prompt-submit")).toBeVisible();

  await test.step("clearing every key blocks the save and says why", async () => {
    // The controller rejects an empty map — "at least one template key is required" —
    // so this is its own rule stated before the request rather than a second opinion.
    for (const index of [0, 1, 2]) await fragmentKey(page, index).fill("");

    await page.getByTestId("prompt-submit").click();
    await expect(page.getByTestId("prompt-form-errors")).toContainText(
      "Add at least one fragment key",
    );
    // Still on the form, with the text intact: a refused save must not read as a
    // save, and must not navigate as one either.
    await expect(page).toHaveURL(/\/edit$/);
    await expect(fragmentValue(page, 2)).toHaveValue(/Be concise/);
  });

  await test.step("two rows sharing a key are named, not silently merged", async () => {
    await fragmentKey(page, 0).fill("tone");
    await fragmentKey(page, 1).fill("tone");

    await page.getByTestId("prompt-submit").click();
    // The payload is a map, so the second value would win and the first fragment
    // would vanish without a word.
    await expect(page.getByTestId("prompt-form-errors")).toContainText(
      'Two fragments share the key "tone"',
    );
  });
});

test("prompt libraries: a library that has gone says so instead of offering a form", async ({
  page,
}) => {
  // Deep-linked, the way a stale tab or a shared address arrives. An edit form over a
  // library the cluster does not have would take input for a save that cannot land.
  await page.goto("/prompts/kagent/not-a-library/edit?mock=ok");
  await expect(page.getByTestId("prompt-edit-not-found")).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByTestId("prompt-submit")).toHaveCount(0);
});

test("prompt libraries: the create form is the same form, and refuses the same things", async ({
  page,
}) => {
  await loadPage(page, routes.prompts, { title: "Prompts" });
  await page.getByTestId("prompts-new").click();
  await expect(page.getByTestId("prompt-submit")).toBeVisible();

  await test.step("the identity is asked for here, unlike an edit", async () => {
    // The other half of the shared form: these two are editable when the library
    // does not exist yet, and marked required because nothing can be created
    // without them.
    await expect(page.getByTestId("prompt-name")).toBeEnabled();
    await expect(page.getByTestId("prompt-namespace")).toBeEnabled();

    await page.getByTestId("prompt-submit").click();
    await expect(page.getByTestId("prompt-form-errors")).toContainText(
      "A library name is required",
    );
  });

  await test.step("leaving a half-typed new library asks first as well", async () => {
    // The baseline is the empty draft rather than a loaded resource, so a library
    // being written for the first time counts as work to lose too.
    await page.getByTestId("prompt-name").fill("half-typed");
    await page.getByTestId("prompt-cancel").click();
    await expect(page.getByTestId("prompt-discard-body")).toBeVisible();
    await page.getByRole("button", { name: "Keep editing" }).click();
    await expect(page).toHaveURL(/\/prompts\/new$/);
  });

  await test.step("a filled-in library is created and appears on the list", async () => {
    await page.getByTestId("prompt-name").fill("release-notes");
    await page.getByTestId("fragment-key").first().fill("changelog");
    await page.getByTestId("fragment-value").first().fill("Group by user impact.");
    await page.getByTestId("prompt-submit").click();

    await expect(page).toHaveURL(/\/prompts$/, { timeout: 30_000 });
    const row = rowNamed(page, "release-notes");
    await expect(row).toContainText("1 key", { timeout: 30_000 });
    await expect(row).toContainText("changelog");
  });
});
