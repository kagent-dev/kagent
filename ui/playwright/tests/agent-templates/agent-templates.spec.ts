import { test, expect } from "../../fixtures/test";
import { dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  confirmation,
  expectRequired,
  selectOption,
} from "../../helpers/resource";

/**
 * Agent templates — the whole life of one, in a single journey.
 *
 * One test rather than several because a video and a trace are recorded per test: a
 * lifecycle split across six of them is one you have to reassemble from six recordings,
 * none of which shows that the state one established is the state the next acted on.
 *
 * ## The property this spec exists for
 *
 * **A template no harness admits cannot be used, and nothing about it looks wrong.** A
 * `Harness` admits templates through a label selector, and the CRD is explicit that a
 * harness with no selector admits none — so a template whose labels match nothing
 * reaches no prepared revision and every `CreateAgentInstance` naming it is refused. It
 * still has a model, a prompt, a row in this list.
 *
 * That was confirmed against a cluster before any of this was built: an unlabelled
 * template sat at `status: {observedGeneration: 1}` with no harnesses at all, and adding
 * the one label its harness selects on took it to *"ActorTemplate golden snapshot is
 * ready"* in about ten seconds.
 *
 * So the "Runs on" column, the warning in the form and the button that applies a
 * harness's labels are the feature, not decoration — and they are what steps 1, 4 and 5
 * cover.
 *
 * ## The second property
 *
 * **Reading a template is not the same act as changing one.** A row used to open the
 * edit form, so looking at a template put the reader in a page of inputs with Save
 * waiting. It now opens a details page with editing as a mode, which is why steps 6 and
 * 7 assert the *reading* state as well as the writing one — and why they assert both are
 * the same component, since a separate read-only view is what would drift.
 *
 * ## One thing the ordering buys, and one it costs
 *
 * The mock backend keeps writes in the page's own memory, so a `page.goto` starts a
 * backend that has never heard of the template just made. Everything from step 5 onwards
 * therefore clicks through rather than navigating, and the created template survives all
 * the way to the delete that removes it. The cost is that a failure stops the steps after
 * it; the recording shows where.
 */

/** The one this journey makes, reads, edits and removes. */
const CREATED = "browser-made";

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("agent templates: a template is created, read, edited and deleted", async ({
  page,
}) => {
  await test.step("1. the list says which templates anything will actually run", async () => {
    await loadPage(page, routes.agentTemplates, { title: "Agents" });
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await expectSettled(page);

    const row = rowNamed(page, "k8s-agent-7f3a91c");
    await expect(row).toContainText("k8s-agent");
    await expect(row).toContainText("default-model-config");

    // `note-taker` carries no labels at all. It is a complete, valid template that can
    // never become an agent, and only this column says so.
    const unusable = page.getByTestId("template-unusable-note-taker");
    await expect(unusable).toBeVisible();
    await expect(unusable).toContainText("No harness");
  });

  await test.step("2. the list narrows like every other landing page", async () => {
    /*
     * This page was the odd one out. It picked a single namespace — `kagent` if it
     * existed, otherwise the first — and offered a dropdown to change it, so a template
     * in a namespace the reader had not selected was not "filtered out": it had never
     * been read, and nothing on screen said so.
     */
    await expect(page.getByTestId("templates-filters")).toContainText("All namespaces");
    await expect(page.getByTestId("templates-filters-pills")).toHaveCount(0);

    const total = await dataRows(page).count();
    await page.getByTestId("templates-filters-search").fill("note-taker");
    await expect(dataRows(page)).toHaveCount(1);
    // The count says what was narrowed from, so "1" cannot be mistaken for "all".
    await expect(page.getByTestId("templates-summary")).toContainText(`of ${total}`);

    // The term is in the address, so the view can be sent to somebody.
    expect(page.url()).toContain("note-taker");
    await page.reload();
    await expect(dataRows(page)).toHaveCount(1, { timeout: 30_000 });
    await expect(page.getByTestId("templates-filters-search")).toHaveValue("note-taker");
  });

  await test.step("3. columns sort", async () => {
    // The search is cleared first: sorting one row proves nothing.
    await page.getByTestId("templates-filters-search").fill("");
    const first = async () => (await dataRows(page).first().textContent()) ?? "";
    const before = await first();
    await page.getByRole("columnheader", { name: /Template/ }).click();
    await expect.poll(first).not.toBe(before);
  });

  await test.step("4. the form says what a template is, and refuses an unusable one", async () => {
    await page.getByTestId("agents-new-template").click();
    await page.waitForURL(/\/agent-templates\/new(\?|$)/);

    // A reader who does not know a template is *half* of an agent cannot tell why the
    // form has no way to run it.
    await expect(page.getByTestId("template-form-explainer")).toContainText(
      "not where it runs",
    );

    const admission = page.getByTestId("template-form-admission");
    await expect(admission).toContainText("No harness will run this template");
    // And it says the template itself is fine, because it is — the reader should not go
    // looking for a mistake in their model or prompt.
    await expect(admission).toContainText("Nothing is wrong with the template itself");

    // `spec.modelConfig` is the one field the CRD requires.
    await expect(page.getByTestId("template-submit")).toBeDisabled();
    await expect(page.getByTestId("template-form-problems")).toContainText(
      "model configuration is required",
    );

    /*
     * And the marks agree with that gate — which this form, of all of them, has to be
     * checked for. antd draws the asterisk from `required` on a `Form.Item` while
     * `draftProblems` refuses the submit in code, and the two came apart here first:
     * the whole agent-template form carried no mark at all while refusing to save
     * without a model configuration. That is one of the two regressions `expectRequired`
     * was written for, so leaving this form the only one not using it would be the
     * worst possible omission.
     *
     * Everything else here is genuinely optional, including the system prompt — a
     * template may take one from its harness instead.
     */
    await expectRequired(page, {
      marked: ["Name", "Model configuration"],
      unmarked: ["Description", "System prompt"],
    });
  });

  await test.step("5. one button makes it admissible, and it saves", async () => {
    await page.getByTestId("template-form-name").fill(CREATED);
    await selectOption(page, "template-form-model", "default-model-config");

    // The step that is easiest to miss, and the one that makes a template usable: it
    // applies whatever labels that harness's selector matches on.
    await page.getByTestId("template-form-admit-k8s-agent").click();
    await expect(page.getByTestId("template-form-admission")).toContainText(
      "admitted by k8s-agent",
    );

    await expect(page.getByTestId("template-submit")).toBeEnabled();
    await page.getByTestId("template-submit").click();
    await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 30_000 });

    const row = rowNamed(page, CREATED);
    await expect(row).toBeVisible({ timeout: 30_000 });
    // The admission is the controller's answer, recomputed from the labels — not an
    // echo of what the form claimed.
    await expect(row).toContainText("k8s-agent");

    /*
     * And the list comes back narrowed to the namespace that was being worked in.
     *
     * Nothing asserted this, which is how two faults sat on the one line that asks for
     * it: `/agent-templates` is a redirect and carries no query string, and the list
     * narrows on `ns` while the caller was sending `namespace`. Either alone would have
     * been enough to lose the filter, and the page looked reasonable both ways.
     */
    await expect(page).toHaveURL(/[?&]ns=kagent(&|$)/);
    await expect(page.getByTestId("templates-filters-pill-ns-kagent")).toBeVisible();
  });

  await test.step("6. a row opens a page that reads, with editing behind a button", async () => {
    await page.getByTestId(`template-link-${CREATED}`).click();
    await page.waitForURL(new RegExp(`/agent-templates/kagent/${CREATED}`));

    // No Save waiting on a reader who came to look. This is the whole item: a form with
    // a submit button says the values on it are provisional, and they are the cluster's.
    await expect(page.getByTestId("template-submit")).toHaveCount(0);
    await expect(page.getByTestId("template-edit")).toBeVisible();

    /*
     * The id lands on antd's inner `<input>`, so this locator *is* the input — and a
     * read-only input is what proves the same component is being used rather than a
     * second view that could drift from it.
     *
     * The description rather than the name: the name field exists only while creating,
     * because a Kubernetes object name cannot be changed afterwards, so on this page the
     * template's identity is the heading instead.
     */
    await expect(page.getByTestId("template-form-description")).toHaveAttribute(
      "readonly",
      "",
    );
    await expect(page.getByTestId("template-form-name")).toHaveCount(0);
    // And nothing that authors: the add buttons only exist when something can be added.
    await expect(page.getByTestId("template-form-add-label")).toHaveCount(0);

    // Nor does it mark anything required. Read-only is not a form, so it asks for
    // nothing — an asterisk here would be demanding a reader supply something they are
    // only looking at, on a template that already has it.
    await expect(page.locator(".ant-form-item-label label").first()).toBeVisible();
    await expect(
      page.locator(".ant-form-item-label label.ant-form-item-required"),
    ).toHaveCount(0);

    // The single most important fact about a template, and the one nothing about the
    // template itself reveals. A reader who has to press Edit to find it will not.
    await expect(page.getByTestId("template-admission-status")).toContainText(
      "k8s-agent",
    );
  });

  await test.step("7. the Agents tab lists the pairs this template is half of", async () => {
    await page.getByRole("tab", { name: /Agents/ }).click();
    // One row per admitting harness. A pair is the durable runnable thing — the template
    // alone does nothing, and an instance is one conversation with a pair.
    await expect(page.getByTestId("template-agents-table")).toContainText("k8s-agent");
    /*
     * Counted, which the pair list alone cannot tell you: a pair exists the moment a
     * harness admits the template, whether or not anyone has ever talked to it. So
     * "which harnesses run this" and "is anything using this" are different questions,
     * and only the second one stops a reader deleting something in use.
     */
    await expect(page.getByTestId("template-pair-conversations").first()).toContainText(
      /\d+ conversations?/,
    );
  });

  await test.step("8. Edit turns the same fields into the form, in place", async () => {
    await page.getByRole("tab", { name: "Details" }).click();
    await page.getByTestId("template-edit").click();
    await expect(page.getByTestId("template-submit")).toBeVisible();
    await expect(page.getByTestId("template-form-description")).not.toHaveAttribute(
      "readonly",
      "",
    );
    // The authoring controls are back, which is what "the same component in two modes"
    // means in practice.
    await expect(page.getByTestId("template-form-add-label")).toBeVisible();
  });

  await test.step("9. leaving edit mode with a draft asks before throwing it away", async () => {
    await page.getByTestId("template-form-description").fill("Edited by the suite.");
    // Visible from either tab, because a draft survives a tab switch and a reader who
    // wandered off should still be able to see there is one.
    await expect(page.getByTestId("template-unsaved")).toBeVisible();

    await page.getByTestId("template-stop-editing").click();
    await expect(page.getByTestId("template-discard-body")).toContainText(
      "have not been saved",
    );
    await page.getByRole("button", { name: "Keep editing" }).click();
    // Kept, not lost — the point of asking.
    await expect(page.getByTestId("template-form-description")).toHaveValue(
      "Edited by the suite.",
    );
  });

  await test.step("10. saving returns to reading, showing what was saved", async () => {
    await page.getByTestId("template-submit").click();
    await expect(page.getByTestId("template-edit")).toBeVisible({ timeout: 30_000 });
    // Read back from the re-read template rather than from the draft: a save that did
    // not reach the backend would leave the old value here.
    await expect(page.getByTestId("template-form-description")).toHaveValue(
      "Edited by the suite.",
    );
  });

  await test.step("11. deleting says what it costs, in the confirmation", async () => {
    // Before opening it: the consequence is nowhere on the page. That is the half of
    // this property the confirmation itself cannot demonstrate — a warning a reader can
    // walk past on the way to the button is a warning they will walk past.
    await expect(page.locator("body")).not.toContainText("keep working");

    // In the header, beside Edit and Back, rather than at the foot of the page. A
    // destructive action a reader only reaches by scrolling past everything else reads
    // as a footnote.
    const deleteButton = page.getByTestId(`delete-${CREATED}`);
    await expect(deleteButton).toContainText("Delete template");
    await deleteButton.click();

    /*
     * Measured against the controller, not read off the schema. A scratch template with
     * a live pair was deleted over gRPC on a cluster: the call was accepted, the
     * resource went, and the `agent_template_harness_pair` row survived in Postgres with
     * `retired_at` set — retired, not removed. The revision collector skips any revision
     * an `agent_instance.prepared_revision` points at before the `ON DELETE RESTRICT` on
     * that column could fire, so an agent's revision is retained *for it*; and
     * `GetLatestRuntimeRevisionForInstance` requires `retired_at IS NULL`, which is what
     * stops anything new being cut from the template afterwards.
     *
     * **What it must not say is that the agents keep running.** A (template, harness)
     * pair *is* an agent here, and deleting the template retires the pair — that is
     * exactly the mechanism that stops new work. What survives is the conversations
     * already open, each holding a revision retained for it.
     */
    const consequence = page.getByTestId("template-delete-consequence");
    await expect(consequence).toContainText("1 agent is built from this template");
    await expect(consequence).toContainText(
      "Conversations already open with it keep working",
    );
    await expect(consequence).toContainText("no new one can be started");
  });

  await test.step("12. confirming removes it, and the list that opens does not show it", async () => {
    // Scoped to the visible popconfirm: every row's confirmation is in the DOM at once,
    // so an unscoped Delete can answer a prompt nobody is looking at.
    await confirmation(page).getByRole("button", { name: "Delete" }).click();
    await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 30_000 });

    // The claim worth making. The list is cached, so landing on it without re-reading
    // shows the template that was just removed — which reads as a delete that silently
    // failed, and is the reason the page invalidates before navigating.
    await expect(rowNamed(page, CREATED)).toHaveCount(0, { timeout: 30_000 });
    // And the rest of the list is intact, so "gone" means that one rather than the read.
    await expect(rowNamed(page, "k8s-agent-7f3a91c")).toBeVisible();
    await expect(page).toHaveURL(/[?&]ns=kagent(&|$)/);
  });

  await test.step("13. a template nothing runs says that instead", async () => {
    // The other branch of the same sentence. Telling a reader that conversations will
    // keep working when no harness ever admitted the template would be noise dressed as
    // care.
    await page.getByTestId("templates-filters-search").fill("note-taker");
    await page.getByTestId("template-link-note-taker").click();
    await page.waitForURL(/\/agent-templates\/kagent\/note-taker/);

    await page.getByTestId("delete-note-taker").click();
    await expect(page.getByTestId("template-delete-consequence")).toContainText(
      "no agent was ever built from it",
    );
  });

  await test.step("14. an agent in the Agents tab opens that agent", async () => {
    // The tab answers "what is built from this template", and each answer is a
    // (template, harness) pair — which is what an agent is. Leaving the rows as text
    // made it a dead end: it named the thing the reader wanted and gave them no way to
    // reach it.
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Back to templates" }).click();
    await page.waitForURL(/\/agents\?.*tab=templates/);
    await page.getByTestId("templates-filters-search").fill("k8s-agent-7f3a91c");
    await page.getByTestId("template-link-k8s-agent-7f3a91c").click();
    await page.waitForURL(/\/agent-templates\/kagent\/k8s-agent-7f3a91c/);

    // The read-only fields carry the template's own values, which is what proves this is
    // the authoring component in another mode rather than a second view that could drift
    // from it. Asserted on this template because the one the journey made has nothing in
    // the field.
    const description = page.getByTestId("template-form-description");
    await expect(description).toHaveAttribute("readonly", "");
    await expect(description).toHaveValue(
      "Answers questions about workloads in the cluster.",
    );

    // `k8s-agent-7f3a91c` carries a `skills` entry, which this form does not author. A
    // save built from the fields it shows would remove it, and the API would accept that
    // without a word — so the notice standing here is what tells a reader it survives.
    // Asserted on this template rather than on the one the journey made, because the
    // form cannot create a template that has anything to lose.
    await expect(page.getByTestId("template-form-unshown")).toContainText(
      "does not remove them",
    );

    await page.getByRole("tab", { name: /Agents/ }).click();
    await page.getByTestId("template-agent-link-k8s-agent").click();

    // The agent's own page, addressed as the pair it is. It has no heading or actions
    // row of its own: the rail names the agent and holds both, so the offer of a new
    // conversation is what says the page arrived.
    await page.waitForURL(/\/agents\/kagent\/k8s-agent-7f3a91c\/on\/k8s-agent$/);
    await expect(page.getByTestId("chat-new-session")).toBeVisible({ timeout: 30_000 });
  });
});
