import type { Page } from "@playwright/test";
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
  anyDialog,
  chooseFilter,
  clickRefresh,
  expectRequired,
  pressUntil,
} from "../../helpers/resource";
import { operationCallCounts, operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * Prompt libraries — the whole life of one, in a single journey.
 *
 * One test, because a video and a trace are recorded per *test* — see
 * `playwright/README.md`.
 *
 * ## The two things a fragment list has that other resources do not
 *
 * **A save replaces the library.** `UpdatePromptTemplate` assigns the ConfigMap's whole
 * `data` map, so removing a row deletes a fragment and two rows sharing a key silently
 * merge into one. Nothing on screen would tell a reader that, so the form says it and
 * refuses both — asserted in step 8, before the save that would otherwise lose prose
 * somebody wrote.
 *
 * **The reads are scoped per namespace.** `ListPromptTemplates` requires a namespace
 * and offers no wildcard, so `usePrompts` fans out one call per namespace rather than
 * narrowing something already fetched. That makes the filter in step 5 a claim about
 * what was *asked for*, and it is also why the timeouts here are longer than the
 * default: under the slow scenario the page waits on the namespace list and then on one
 * call per namespace.
 */

/** The two seeded libraries. */
const SEEDED = ["shared-fragments", "incident-playbooks"];

/** `kagent/shared-fragments`, whose three keys sort into this order. */
const SEEDED_KEYS = ["escalation", "safety", "tone"];

/** The one this journey writes, reads, changes and removes. */
const CREATED = "browser-made-library";

/** The nth fragment row's key and text boxes. */
const fragmentKey = (page: Page, index: number) =>
  page.getByTestId("fragment-key").nth(index);
const fragmentValue = (page: Page, index: number) =>
  page.getByTestId("fragment-value").nth(index);

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("prompts: libraries are read, edited, and their failures reported", async ({
  page,
}) => {
  await test.step("1. a loading state precedes the data", async () => {
    await expectLoading(page, routes.prompts);
  });

  await test.step("2. every library is listed with its key count", async () => {
    for (const name of SEEDED) {
      await expect(rowNamed(page, name), `"${name}" is missing`).toHaveCount(1, {
        timeout: 30_000,
      });
    }
    await expect(dataRows(page)).toHaveCount(SEEDED.length);

    const shared = rowNamed(page, "shared-fragments");
    await expect(shared).toContainText("kagent");
    await expect(shared).toContainText("3 keys");
    await expectSettled(page);
  });

  await test.step("3. opening a library shows each fragment and how to include it", async () => {
    await rowNamed(page, "shared-fragments").getByRole("link").first().click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments$/);

    const fragments = page.getByTestId("prompt-fragments");
    await expect(fragments).toBeVisible();
    // The include expression is the thing a reader came for — it is what they paste
    // into a system message, and it appears nowhere else in the app.
    await expect(fragments).toContainText('{{include "shared-fragments/tone"}}');
    await expect(fragments).toContainText("tone");
    await expect(fragments).toContainText("safety");
    await expect(page.getByTestId("prompt-detail-meta")).toContainText("kagent");
  });

  await test.step("4. refreshing re-reads the list, and says that it did", async () => {
    /*
     * Back on `ok` first, and that is the point rather than housekeeping: the scenario
     * persists for the browsing session, so a refresh clicked while `slow` is still in
     * force waits 2.5 seconds *per call* — and this page fans out one call per
     * namespace, which put the confirmation well past any sensible timeout. It failed
     * exactly that way, on both engines, and reads as a missing feature rather than a
     * slow one.
     */
    await loadPage(page, routes.prompts, { scenario: "ok", title: "Prompts" });
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(1, {
      timeout: 30_000,
    });

    // A refresh usually returns the same rows, so a successful one is otherwise
    // indistinguishable from a button that did nothing. Counted rather than read off
    // the toast, which lives two seconds.
    const before = await operationCalls(page, rpc.listPromptTemplates);
    await clickRefresh(page);
    await expect
      .poll(() => operationCalls(page, rpc.listPromptTemplates), { timeout: 10_000 })
      .toBeGreaterThan(before);
  });

  await test.step("5. the namespace filter is asked of the server, not applied after", async () => {
    await chooseFilter(page, "prompts-filters-filter-ns", "platform");
    await expectSettled(page);
    await expect(rowNamed(page, "incident-playbooks")).toHaveCount(1);
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(0);

    // With the read scoped, the page has not asked about the other namespaces — so it
    // cannot claim a total, and says "read" rather than implying one.
    await expect(page.getByTestId("prompts-summary")).toContainText(
      "1 of 1 library read",
    );

    // The distinction the scoped read forces. "No prompt libraries yet" would be a
    // claim about the cluster that this page, having asked about one namespace, is in
    // no position to make.
    await page.goto("/prompts?mock=ok&ns=analytics");
    await expectSettled(page);
    await expect(
      page.getByText("No prompt libraries match those filters."),
    ).toBeVisible();
    await expect(page.getByText("No prompt libraries yet.")).toHaveCount(0);
  });

  await test.step("6. the create form asks for the identity an edit cannot change", async () => {
    await loadPage(page, routes.prompts, { title: "Prompts" });
    await page.getByTestId("prompts-new").click();
    await expect(page.getByTestId("prompt-submit")).toBeVisible();

    await expectRequired(page, { marked: ["Namespace", "Name"], unmarked: [] });
    // Editable here, because the library does not exist yet. On an edit the same two
    // fields are read-only — step 7 is where that is asserted.
    for (const field of ["prompt-name", "prompt-namespace"]) {
      await expect(page.getByTestId(field)).toBeEnabled();
      await expect(page.getByTestId(field)).not.toHaveAttribute("readonly", "");
    }

    await page.getByTestId("prompt-submit").click();
    await expect(page.getByTestId("prompt-form-errors")).toContainText(
      "A library name is required",
    );

    // Half-typed counts as work to lose too: the baseline is the empty draft rather
    // than a loaded resource, so a library being written for the first time is guarded
    // the same way an edit is.
    await page.getByTestId("prompt-name").fill(CREATED);
    await page.getByTestId("prompt-cancel").click();
    // The body rather than the modal: antd hangs the outer testid on a wrapper it keeps
    // hidden, so asserting on that one passes without the dialog being up.
    await expect(page.getByTestId("prompt-discard-body")).toBeVisible();
    // Pressed until it takes — see `pressUntil`. A dismissal aimed at a dialog that is
    // still animating in is dropped, and this is one of the two sites where that was
    // measurably happening.
    await pressUntil(page.getByRole("button", { name: "Keep editing" }), async () => {
      // Waited out rather than assumed gone: a field filled while the dialog is still
      // closing can be re-rendered back to its previous value by the state change that
      // closes it.
      await expect(page.getByTestId("prompt-discard-body")).toBeHidden();
    });
    await expect(page).toHaveURL(/\/prompts\/new$/);

    await expect(page.getByTestId("prompt-name")).toHaveValue(CREATED);

    /*
     * And that is where this step stops. Creating the library, reading it back, adding a
     * fragment and deleting it runs against both backends from
     * `playwright/shared/prompts/prompts.spec.ts`. Left here it was the same journey
     * driven twice, and the count it used to assert is stronger there, where it is
     * relative to whatever the list already held.
     *
     * Navigated rather than submitted, so the steps below start on the list with a draft
     * abandoned — which is also the state a reader leaves behind when they change their
     * mind, and nothing else covers it.
     */
    await loadPage(page, routes.prompts, { title: "Prompts" });
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });

  await test.step("7. the list's edit action opens the form, not the reading page", async () => {
    // It used to land on the read-only detail page: it promised an edit and delivered
    // the place the library's name already opened, while `UpdatePromptTemplate` — built
    // end to end, from the RPC down to the ConfigMap write — was reachable from nowhere
    // in the app.
    await page.getByTestId("edit-shared-fragments").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments\/edit$/);
    await expect(page.getByTestId("prompt-submit")).toBeVisible();

    // `data` is a map on the wire, so the order is the app's to impose and the reading
    // page and the form have to agree on it.
    await expect(page.getByTestId("fragment-row")).toHaveCount(SEEDED_KEYS.length);
    for (const [index, key] of SEEDED_KEYS.entries()) {
      await expect(fragmentKey(page, index)).toHaveValue(key);
    }
    await expect(fragmentValue(page, 2)).toHaveValue(/Be concise/);

    /*
     * The identity is locked, because the ref addresses the ConfigMap: an edit cannot
     * rename a library or move it to another namespace.
     *
     * Read-only rather than disabled, and the difference is the point: these two are
     * shown so the reader is sure which library they are editing, and a disabled field
     * is dimmed. Both attributes are asserted so a change back to `disabled` fails here
     * rather than only being noticed as a colour.
     */
    for (const field of ["prompt-name", "prompt-namespace"]) {
      await expect(page.getByTestId(field)).toHaveAttribute("readonly", "");
      await expect(page.getByTestId(field)).toBeEnabled();
    }

    // And the form says a save replaces the library, because it does.
    await expect(page.getByTestId("prompt-fragments-note")).toContainText("is deleted");
  });

  await test.step("8. an edit that would silently lose a fragment is refused", async () => {
    // The controller rejects an empty map — "at least one template key is required" —
    // so this is its own rule stated before the request rather than a second opinion.
    for (const index of [0, 1, 2]) await fragmentKey(page, index).fill("");
    await page.getByTestId("prompt-submit").click();
    await expect(page.getByTestId("prompt-form-errors")).toContainText(
      "Add at least one fragment key",
    );
    // Still on the form, with the text intact: a refused save must not read as a save,
    // and must not navigate as one either.
    await expect(page).toHaveURL(/\/edit$/);
    await expect(fragmentValue(page, 2)).toHaveValue(/Be concise/);

    // The payload is a map, so the second value would win and the first fragment would
    // vanish without a word.
    await fragmentKey(page, 0).fill("tone");
    await fragmentKey(page, 1).fill("tone");
    await page.getByTestId("prompt-submit").click();
    await expect(page.getByTestId("prompt-form-errors")).toContainText(
      'Two fragments share the key "tone"',
    );

    // Put back, so the save in step 9 is a save of the library rather than of a wreck.
    await fragmentKey(page, 0).fill(SEEDED_KEYS[0]);
    await fragmentKey(page, 1).fill(SEEDED_KEYS[1]);
    await fragmentKey(page, 2).fill(SEEDED_KEYS[2]);
  });

  await test.step("9. leaving with a draft asks first, on every way out", async () => {
    await fragmentValue(page, 2).fill("Edited by the suite.");

    /*
     * The Cancel button, the header link and the sidebar. The last two are where a
     * guard on Cancel alone fails: it teaches a reader the work is held safe and then
     * the two more obvious exits throw it away without a word. Both are ordinary links,
     * so what is asserted is that leaving is blocked rather than that a button asks.
     */
    for (const leave of [
      page.getByTestId("prompt-cancel"),
      page.getByRole("link", { name: "Back to library" }),
      page.getByRole("link", { name: "Models" }),
    ]) {
      await leave.click();
      await expect(page.getByTestId("prompt-discard-body")).toBeVisible();

      await pressUntil(page.getByRole("button", { name: "Keep editing" }), async () => {
        await expect(page.getByTestId("prompt-discard-body")).toBeHidden();
      });

      // Waited out before the next exit is tried: the dialog's overlay outlives the
      // click that dismissed it, and swallows whatever is aimed at the page beneath.
      await expect(anyDialog(page)).toBeHidden();
      await expect(page).toHaveURL(/\/edit$/);
      // Kept, not lost — the point of asking. A prompt fragment is prose somebody
      // wrote, which is exactly the thing a silent discard costs most.
      await expect(fragmentValue(page, 2)).toHaveValue("Edited by the suite.");
    }
  });

  await test.step("10. a fragment is added, saved, and read back off the library", async () => {
    await page.getByTestId("fragment-add").click();
    await fragmentKey(page, 3).fill("handoff");
    await fragmentValue(page, 3).fill("Name the next owner explicitly.");
    // The include tag is what the fragment is for, and it is offered before the save
    // rather than only after it.
    await expect(page.getByTestId("fragment-include-preview").last()).toContainText(
      '{{include "shared-fragments/handoff"}}',
    );

    // A save is not itself treated as leaving with unsaved work: the draft stops being
    // unsaved before the caller navigates. This URL assertion is what would fail if it
    // were.
    await page.getByTestId("prompt-submit").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments$/, {
      timeout: 30_000,
    });

    const fragments = page.getByTestId("prompt-fragments");
    // Read back from the re-read library rather than from the draft: a save that never
    // reached the backend would leave the old text here.
    await expect(fragments).toContainText("Edited by the suite.");
    await expect(fragments).toContainText("Name the next owner explicitly.");
    await expect(page.getByTestId("prompt-detail-meta")).toContainText("4 fragments");
  });

  await test.step("11. the list behind it was re-read, and nothing else moved", async () => {
    await page.getByRole("link", { name: "Back to libraries" }).click();
    const row = rowNamed(page, "shared-fragments");
    await expect(row).toContainText("4 keys", { timeout: 30_000 });
    // The keys column too, which is what the search on this page also covers.
    await expect(row).toContainText("handoff");

    await expect(dataRows(page)).toHaveCount(SEEDED.length);
    await expect(rowNamed(page, "incident-playbooks")).toContainText("2 keys");
  });

  await test.step("12. the library's own Edit action reaches the same form", async () => {
    // Two ways in, and only one of them was ever wired up correctly — the list's action
    // used to land on the read-only page. Both are asserted because they are separate
    // call sites, and a form reached one way and not the other is the shape of the
    // original defect.
    await rowNamed(page, "shared-fragments").getByRole("link").first().click();
    await page.getByTestId("prompt-edit").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments\/edit$/);

    // Seeded from the saved library, so the form and the page it came from agree.
    await expect(page.getByTestId("fragment-row")).toHaveCount(4);
    await expect(fragmentKey(page, 1)).toHaveValue("handoff");

    // And Cancel with nothing typed goes straight back, with nothing to confirm: a
    // question over a form nobody has touched is a question that teaches readers to
    // click through the next one.
    await page.getByTestId("prompt-cancel").click();
    await expect(page).toHaveURL(/\/prompts\/kagent\/shared-fragments$/);
    await expect(page.getByTestId("prompt-discard-body")).toHaveCount(0);

    await page.getByRole("link", { name: "Back to libraries" }).click();
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(1, { timeout: 30_000 });
  });

  await test.step("13. a library that has gone says so instead of offering a form", async () => {
    // Deep-linked, the way a stale tab or a shared address arrives. An edit form over a
    // library the cluster does not have would take input for a save that cannot land.
    await page.goto("/prompts/kagent/not-a-library/edit?mock=ok");
    await expect(page.getByTestId("prompt-edit-not-found")).toBeVisible({
      timeout: 30_000,
    });
    await expect(page.getByTestId("prompt-submit")).toHaveCount(0);

    // And on the reading page, a library that does not exist is told apart from one
    // that failed: nothing is wrong with the backend here, so reporting a failure would
    // send a reader looking for a fault that is not there.
    await page.goto("/prompts/kagent/no-such-library?mock=ok");
    await expect(page.getByTestId("prompt-detail-not-found")).toBeVisible();
    await expect(page.getByTestId("prompt-detail-error")).toHaveCount(0);
  });

  await test.step("14. an empty result says so instead of showing a bare table", async () => {
    await loadPage(page, routes.prompts, { scenario: "empty", title: "Prompts" });
    await expect(page.getByText("No prompt libraries yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("15. a failed load is reported, not disguised as an empty list", async () => {
    await loadPage(page, routes.prompts, { scenario: "error", title: "Prompts" });

    const alert = page.getByTestId("prompts-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load prompt libraries");
    // The backend's own account of the failure reaches the reader rather than a generic
    // message.
    await expect(alert).toContainText("asked to fail");

    await expect(page.getByText("No prompt libraries yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);

    // A single library that cannot be read is a different failure from a list that
    // cannot, and leads a reader to a different action — so the detail page reports its
    // own, and shows no fragments beside it. A partial fragment list under an error
    // would invite copying an include for something that may not exist.
    await page.goto("/prompts/kagent/shared-fragments?mock=error");
    const detail = page.getByTestId("prompt-detail-error");
    await expect(detail).toBeVisible();
    await expect(detail).toContainText("Could not load this prompt library");
    await expect(page.getByTestId("prompt-fragments")).toHaveCount(0);
  });

  await test.step("16. retrying asks the backend again, and it recovers", async () => {
    await loadPage(page, routes.prompts, { scenario: "error", title: "Prompts" });

    /*
     * Either read counts as the retry. Listing across namespaces starts with the
     * namespaces call and fans out from there, so under a failing backend the retry
     * does not reach `ListPromptTemplates` at all — it fails at the first hop. Watching
     * only that one would report "no retry happened" when a retry demonstrably did.
     */
    const WATCHED = [rpc.listPromptTemplates, rpc.listNamespaces] as const;
    const total = async () => {
      const counts = await operationCallCounts(page, WATCHED);
      return WATCHED.reduce((sum, read) => sum + counts[read], 0);
    };

    const before = await total();
    await page.getByRole("button", { name: "Try again" }).click();
    await expect.poll(total, { timeout: 10_000 }).toBeGreaterThan(before);

    // A failure that cannot clear is indistinguishable from a broken page, so the
    // recovery is as much a part of the contract as the message.
    await loadPage(page, routes.prompts, { title: "Prompts" });
    await expect(page.getByTestId("prompts-error")).toHaveCount(0);
    await expect(rowNamed(page, "shared-fragments")).toHaveCount(1, {
      timeout: 30_000,
    });
  });
});
