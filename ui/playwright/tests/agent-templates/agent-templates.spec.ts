import { test, expect } from "../../fixtures/test";
import { dataRows, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  confirmation,
  expectRequired,
  pressOnce,
  selectOption,
} from "../../helpers/resource";

/** A reusable template can be authored without choosing an Agent or Harness. */
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

    await expect(row).toContainText("default-model-config");

    // `note-taker` carries no labels at all. It is a complete, valid template that can
    // never become an agent, and only this column says so.

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

    await test.step("an MCP binding can require approval before a tool runs", async () => {
      await page.getByTestId("template-form-add-mcp").click();
      const approval = page.getByRole("checkbox", { name: "Require approval" });
      await expect(approval).toBeVisible();
      await approval.check();
      await expect(approval).toBeChecked();
      // Drop the unfinished row so the rest of the create is unchanged.
      await page.getByTestId("template-form-mcp-remove-0").click();
    });


    // Only the name is required: a BYO harness runs a template with no model.
    await expect(page.getByTestId("template-submit")).toBeDisabled();
    await expect(page.getByTestId("template-form-problems")).toContainText("A name is required");
    await expectRequired(page, {
      marked: ["Name"],
      unmarked: ["Model configuration", "Description", "System prompt"],
    });
  });

  await test.step("5. the template saves without creating an Agent", async () => {
    await page.getByTestId("template-form-name").fill(CREATED);
    await selectOption(page, "template-form-model", "default-model-config");

    await expect(page.getByTestId("template-submit")).toBeEnabled();
    await page.getByTestId("template-submit").click();
    await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 30_000 });

    const row = rowNamed(page, CREATED);
    await expect(row).toBeVisible({ timeout: 30_000 });
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

  });

  await test.step("7. saving a template does not create an Agent", async () => {
    await page.getByRole("tab", {name: /Agents/}).click();
    await expect(page.getByTestId("template-agents-table")).toContainText("No Agent directly references this template.");
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
    /*
     * Pressed once it has stopped arriving, then checked that it went.
     *
     * The check earns its place: `toHaveValue` below reads a field it does not need to
     * see, so without it this step passes with the prompt still over the page and step
     * 10 spends its whole budget failing to click Save through `.ant-modal-wrap`.
     */
    await pressOnce(page.getByRole("button", { name: "Keep editing" }));
    await expect(page.getByTestId("template-discard-body")).toBeHidden();
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

    // Existing revisions survive deletion; future configuration needs this reference.
    const consequence = confirmation(page);
    await expect(consequence).toContainText("Existing conversations");
    await expect(consequence).toContainText(
      "last successful revisions are retained",
    );
    await expect(consequence).toContainText("cannot prepare new configuration");
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

  await test.step("13. deletion also works for an unused template", async () => {
    await page.getByTestId("templates-filters-search").fill("note-taker");
    await page.getByTestId("template-link-note-taker").click();
    await page.waitForURL(/\/agent-templates\/kagent\/note-taker/);

    await page.getByTestId("delete-note-taker").click();
    await expect(confirmation(page)).toContainText(
      "last successful revisions are retained",
    );
  });

  await test.step("14. an agent in the Agents tab opens that agent", async () => {
    // The tab answers "what is built from this template", and each answer is a
    // explicit Agent referencing it. Leaving the rows as text
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
    await page.getByTestId("template-agent-link-k8s-agent-7f3a91c").click();

    // The agent's own page, addressed by its identity. It has no heading or actions
    // row of its own: the rail names the agent and holds both, so the offer of a new
    // conversation is what says the page arrived.
    await page.waitForURL(/\/agents\/kagent\/k8s-agent-7f3a91c$/);
    await expect(page.getByTestId("chat-new-session")).toBeVisible({ timeout: 30_000 });
  });

  await test.step("15. an empty result says so instead of showing a bare table", async () => {
    // Last, after the delete, because reaching these needs the backend answering
    // differently and `?mock=` is per-navigation — which discards what the journey made.
    // By here there is nothing left to discard.
    await loadPage(page, routes.agentTemplates, { scenario: "empty", title: "Agents" });
    await expect(page.getByText("No agent templates yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("16. a failed load is reported, not disguised as an empty list", async () => {
    await loadPage(page, routes.agentTemplates, { scenario: "error", title: "Agents" });

    const alert = page.getByTestId("templates-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load agent templates");
    // The distinction the step exists for: "there are none" and "we could not find out"
    // lead a reader to opposite conclusions, and only one of them is true.
    await expect(page.getByText("No agent templates yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });
});
