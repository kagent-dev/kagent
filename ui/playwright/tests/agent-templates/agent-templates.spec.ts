import { test, expect } from "../../fixtures/test";
import { dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";

/**
 * Agent templates — what an agent does, authored and read on its own pages.
 *
 * ## The property this suite exists for
 *
 * **A template no harness admits cannot be used, and nothing about it looks
 * wrong.** A `Harness` admits templates through a label selector, and the CRD is
 * explicit that a harness with no selector admits none — so a template whose labels
 * match nothing reaches no prepared revision and every `CreateAgentInstance` naming
 * it is refused. It still has a model, a prompt, a row in this list.
 *
 * That was confirmed against a cluster before any of this was built: an unlabelled
 * template sat at `status: {observedGeneration: 1}` with no harnesses at all, and
 * adding the one label its harness selects on took it to *"ActorTemplate golden
 * snapshot is ready"* in about ten seconds.
 *
 * So the "Runs on" column, the warning in the form and the button that applies a
 * harness's labels are the feature, not decoration — and they are what this covers.
 *
 * ## The second property, newer
 *
 * **Reading a template is not the same act as changing one.** A row used to open the
 * edit form, so looking at a template put the reader in a page of inputs with Save
 * waiting. It now opens a details page with editing as a mode, so the specs below
 * assert on the *reading* state as well as the writing one — and on the fact that
 * both are the same component, since a separate read-only view is what would drift.
 */

test("agent templates: the list says which templates anything will actually run", async ({
  page,
}) => {
  await test.step("1. the list loads", async () => {
    await loadPage(page, routes.agentTemplates, { title: "Agents" });
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await expectSettled(page);
  });

  await test.step("2. a template a harness admits says which one", async () => {
    const row = rowNamed(page, "k8s-agent-7f3a91c");
    await expect(row).toContainText("k8s-agent");
    await expect(row).toContainText("default-model-config");
  });

  await test.step("3. a template nothing admits is marked, not left looking fine", async () => {
    // `note-taker` carries no labels at all. It is a complete, valid template that
    // can never become an agent, and only this column says so.
    await expect(page.getByTestId("template-unusable-note-taker")).toBeVisible();
    await expect(page.getByTestId("template-unusable-note-taker")).toContainText(
      "No harness",
    );
  });
});

test("agent templates: creating one, and being told when nothing will run it", async ({
  page,
}) => {
  await test.step("1. the form opens from the list", async () => {
    await loadPage(page, routes.agentTemplates, { title: "Agents" });
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("agents-new-template").click();
    await page.waitForURL(/\/agent-templates\/new(\?|$)/);
  });

  await test.step("2. it says what an agent template is before asking for anything", async () => {
    // A reader who does not know a template is *half* of an agent cannot tell why
    // the form has no way to run it.
    await expect(page.getByTestId("template-form-explainer")).toContainText(
      "not where it runs",
    );
  });

  await test.step("3. an unlabelled draft is warned about, loudly", async () => {
    const admission = page.getByTestId("template-form-admission");
    await expect(admission).toContainText("No harness will run this template");
    // And it says the template itself is fine, because it is — the reader should
    // not go looking for a mistake in their model or prompt.
    await expect(admission).toContainText("Nothing is wrong with the template itself");
  });

  await test.step("4. nothing can be saved until the required field is set", async () => {
    // `spec.modelConfig` is the one field the CRD requires.
    await expect(page.getByTestId("template-submit")).toBeDisabled();
    await expect(page.getByTestId("template-form-problems")).toContainText(
      "model configuration is required",
    );
  });

  await test.step("5. one button makes it admissible by a harness", async () => {
    await page.getByTestId("template-form-name").fill("browser-made");
    await page.getByTestId("template-form-model").click();
    const model = page.locator('.ant-select-item-option[title="default-model-config"]');
    await expect(model).toBeVisible({ timeout: 30_000 });
    await model.click();

    // The step that is easiest to miss, and the one that makes a template usable:
    // it applies whatever labels that harness's selector matches on.
    await page.getByTestId("template-form-admit-k8s-agent").click();
    await expect(page.getByTestId("template-form-admission")).toContainText(
      "admitted by k8s-agent",
    );
  });

  await test.step("6. it saves, and the list reports what will run it", async () => {
    await expect(page.getByTestId("template-submit")).toBeEnabled();
    await page.getByTestId("template-submit").click();
    await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 30_000 });

    const row = rowNamed(page, "browser-made");
    await expect(row).toBeVisible({ timeout: 30_000 });
    // The admission is the controller's answer, recomputed from the labels — not an
    // echo of what the form claimed.
    await expect(row).toContainText("k8s-agent");
  });
});

test("agent templates: a row opens a page that reads, with editing behind a button", async ({
  page,
}) => {
  await test.step("1. clicking a row lands on the template, not in a form", async () => {
    await loadPage(page, routes.agentTemplates, { title: "Agents" });
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("template-link-k8s-agent-7f3a91c").click();
    await page.waitForURL(/\/agent-templates\/kagent\/k8s-agent-7f3a91c/);

    // No Save waiting on a reader who came to look. This is the whole item: a form
    // with a submit button says the values on it are provisional, and they are the
    // cluster's.
    await expect(page.getByTestId("template-submit")).toHaveCount(0);
    await expect(page.getByTestId("template-edit")).toBeVisible();
  });

  await test.step("2. the fields are the template's values, rendered as text", async () => {
    // The id lands on antd's inner `<input>`, so this locator *is* the input — and a
    // read-only input carrying the value is what proves the same component is being
    // used rather than a second view that could drift from it.
    const description = page.getByTestId("template-form-description");
    await expect(description).toHaveAttribute("readonly", "");
    await expect(description).toHaveValue(
      "Answers questions about workloads in the cluster.",
    );
    // And nothing that authors: the add buttons only exist when something can be added.
    await expect(page.getByTestId("template-form-add-label")).toHaveCount(0);
  });

  await test.step("3. whether anything will run it is on the page, not behind Edit", async () => {
    // The single most important fact about a template, and the one nothing about the
    // template itself reveals. A reader who has to press Edit to find it will not.
    await expect(page.getByTestId("template-admission-status")).toContainText(
      "k8s-agent",
    );
  });

  await test.step("4. the Agents tab lists the pairs this template is half of", async () => {
    await page.getByRole("tab", { name: /Agents/ }).click();
    const table = page.getByTestId("template-agents-table");
    // One row per admitting harness. A pair is the durable runnable thing — the
    // template alone does nothing and an instance is one conversation with a pair.
    await expect(table).toContainText("k8s-agent");
    // Counted, which the pair list alone cannot tell you: a pair exists the moment a
    // harness admits the template, whether or not anyone has ever talked to it. So
    // "which harnesses run this" and "is anything using this" are different questions,
    // and only the second one stops a reader deleting something in use.
    //
    // This was a seam until `ListAgentInstances` gained `agent_template`/`harness`
    // filters. It could not be closed with `match_labels`, even though instances do
    // carry labels: admission labels are shared by construction, so filtering on one
    // returns every template that harness admits.
    await expect(page.getByTestId("template-pair-conversations").first()).toContainText(
      /\d+ conversations?/,
    );
  });

  await test.step("5. Edit turns the same fields into the form, in place", async () => {
    await page.getByRole("tab", { name: "Details" }).click();
    await page.getByTestId("template-edit").click();
    await expect(page.getByTestId("template-submit")).toBeVisible();
    await expect(page.getByTestId("template-form-description")).not.toHaveAttribute(
      "readonly",
      "",
    );
    // The authoring controls are back, which is what "the same component in two
    // modes" means in practice.
    await expect(page.getByTestId("template-form-add-label")).toBeVisible();
  });

  await test.step("6. leaving edit mode with a draft asks before throwing it away", async () => {
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

  await test.step("7. saving returns to reading, showing what was saved", async () => {
    await page.getByTestId("template-submit").click();
    await expect(page.getByTestId("template-edit")).toBeVisible({ timeout: 30_000 });
    // Read back from the re-read template rather than from the draft: a save that did
    // not reach the backend would leave the old value here.
    await expect(page.getByTestId("template-form-description")).toHaveValue(
      "Edited by the suite.",
    );
  });

  await test.step("8. and the edit did not delete what the form cannot show", async () => {
    // `k8s-agent-7f3a91c` carries a `skills` entry, which this form does not author.
    // A save built from the fields it shows would remove it, and the API would accept
    // that without a word — so the notice standing here is the evidence it survived.
    await expect(page.getByTestId("template-form-unshown")).toContainText(
      "does not remove them",
    );
  });
});

test("agent templates: deleting one says what it costs, and the list is re-read", async ({
  page,
}) => {
  await test.step("1. a template with an agent on it says so before deleting", async () => {
    await loadPage(page, routes.agentTemplates, { title: "Agents" });
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("template-link-k8s-agent-7f3a91c").click();
    await page.waitForURL(/\/agent-templates\/kagent\/k8s-agent-7f3a91c/);

    // Before opening it: the consequence is nowhere on the page. That is the half of
    // this property the confirmation itself cannot demonstrate — a warning a reader can
    // walk past on the way to the button is a warning they will walk past.
    await expect(page.locator("body")).not.toContainText("keep working");

    // In the header now, beside Edit and Back, rather than at the foot of the page. A
    // destructive action a reader only reaches by scrolling past everything else reads
    // as a footnote.
    const deleteButton = page.getByTestId("delete-k8s-agent-7f3a91c");
    await expect(deleteButton).toContainText("Delete template");
    await deleteButton.click();
    const consequence = page.getByTestId("template-delete-consequence");
    /*
     * Measured against the controller, not read off the schema. A scratch template
     * with a live pair was deleted over gRPC on a cluster: the call was accepted, the
     * resource went, and the `agent_template_harness_pair` row survived in Postgres
     * with `retired_at` set — retired, not removed. The revision collector skips any
     * revision an `agent_instance.prepared_revision` points at before the
     * `ON DELETE RESTRICT` on that column could fire, so an agent's revision is
     * retained *for it*; and `GetLatestRuntimeRevisionForInstance` requires
     * `retired_at IS NULL`, which is what stops anything new being cut from the
     * template afterwards.
     *
     * The sentence is asserted here, inside the confirmation, and nowhere else on the
     * page. A consequence a reader can walk past on the way to the button is one they
     * will walk past; a bare "are you sure?" over an object with live dependents is
     * the prompt that gets clicked through.
     *
     * **What it must not say is that the agents keep running.** A (template, harness)
     * pair *is* an agent here, and deleting the template retires the pair — that is
     * exactly the mechanism that stops new work. What survives is the conversations
     * already open, each holding a revision retained for it. The earlier wording had
     * the agent surviving and the conversations unmentioned, which is the same noun
     * confusion that put instances on the agents page in the first place.
     */
    await expect(consequence).toContainText("1 agent is built from this template");
    await expect(consequence).toContainText("Conversations already open with it keep working");
    await expect(consequence).toContainText("no new one can be started");
    await page.getByRole("button", { name: "Keep" }).click();
  });

  await test.step("2. a template nothing runs says that instead", async () => {
    await page.getByRole("button", { name: "Back to templates" }).click();
    await page.waitForURL(/\/agents\?.*tab=templates/);
    await expect(rowNamed(page, "note-taker")).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("template-link-note-taker").click();
    await page.waitForURL(/\/agent-templates\/kagent\/note-taker/);

    await page.getByTestId("delete-note-taker").click();
    // Different sentence, because it is a different fact — telling a reader that
    // conversations will keep working when no harness ever admitted the template
    // would be noise dressed as care.
    await expect(page.getByTestId("template-delete-consequence")).toContainText(
      "no agent was ever built from it",
    );
  });

  await test.step("3. confirming removes it, and the list that opens does not show it", async () => {
    // Scoped to the visible popconfirm: every row's confirmation is in the DOM at
    // once, so an unscoped Delete can answer a prompt nobody is looking at.
    await page
      .locator(".ant-popconfirm:visible")
      .getByRole("button", { name: "Delete" })
      .click();
    await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 30_000 });

    // The claim worth making. The list is cached, so landing on it without re-reading
    // shows the template that was just removed — which reads as a delete that
    // silently failed, and is the reason the page invalidates before navigating.
    await expect(rowNamed(page, "note-taker")).toHaveCount(0, { timeout: 30_000 });
    // And the rest of the list is intact, so "gone" means that one rather than the read.
    await expect(rowNamed(page, "k8s-agent-7f3a91c")).toBeVisible();
  });
});


test("agent templates: the list narrows like every other landing page", async ({ page }) => {
  /*
   * This page was the odd one out.
   *
   * It picked a single namespace — `kagent` if it existed, otherwise the first — and
   * offered a dropdown to change it. So a template in a namespace the reader had not
   * selected was not "filtered out": it had never been read, and nothing on screen said
   * so. Every other landing page defaults to all namespaces and narrows from there.
   */
  await loadPage(page, routes.agentTemplates, { title: "Agents" });
  await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. all namespaces by default, and no pills", async () => {
    await expect(page.getByTestId("templates-filters")).toContainText("All namespaces");
    await expect(page.getByTestId("templates-filters-pills")).toHaveCount(0);
  });

  await test.step("2. search covers every row that was read, not one page of it", async () => {
    const total = await dataRows(page).count();
    await page.getByTestId("templates-filters-search").fill("note-taker");
    await expect(dataRows(page)).toHaveCount(1);
    await expect(rowNamed(page, "note-taker")).toBeVisible();
    // And the count says what was narrowed from, so "1" cannot be mistaken for "all".
    await expect(page.getByTestId("templates-summary")).toContainText(`of ${total}`);
  });

  await test.step("3. the term is in the address, so the view can be sent to somebody", async () => {
    expect(page.url()).toContain("note-taker");
    await page.reload();
    await expect(dataRows(page)).toHaveCount(1, { timeout: 30_000 });
    await expect(page.getByTestId("templates-filters-search")).toHaveValue("note-taker");
  });

  await test.step("4. columns sort", async () => {
    // The search is cleared first: sorting one row proves nothing.
    await page.getByTestId("templates-filters-search").fill("");
    const first = async () => (await dataRows(page).first().textContent()) ?? "";
    const before = await first();
    await page.getByRole("columnheader", { name: /Template/ }).click();
    await expect.poll(first).not.toBe(before);
  });
});

test("agent templates: an agent in the Agents tab opens that agent", async ({ page }) => {
  // The tab answers "what is built from this template", and each answer is a
  // (template, harness) pair — which is what an agent is. Leaving the rows as text made
  // it a dead end: it named the thing the reader wanted and gave them no way to reach it.
  await loadPage(page, routes.agentTemplates, { title: "Agents" });
  await page.getByTestId("template-link-k8s-agent-7f3a91c").click();
  await page.waitForURL(/\/agent-templates\/kagent\/k8s-agent-7f3a91c/);

  await page.getByRole("tab", { name: /Agents/ }).click();
  await page.getByTestId("template-agent-link-k8s-agent").click();

  // The agent's own page, addressed as the pair it is.
  await page.waitForURL(/\/agents\/kagent\/k8s-agent-7f3a91c\/on\/k8s-agent$/);
  // Its own page, offering what an agent offers — a new conversation with it, from the
  // rail that page carries. The page itself has no heading or actions row: the rail
  // names the agent and holds both.
  await expect(page.getByTestId("chat-new-session")).toBeVisible({ timeout: 30_000 });
});
