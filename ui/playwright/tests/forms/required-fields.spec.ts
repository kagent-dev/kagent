import type { Locator, Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { expectSettled, loadPage, routes } from "../../helpers/app";

/** The fixture configuration the edit form opens on. */
const modelEdit = "/models/kagent/default-model-config/edit";

/** The fixture template the details page renders read-only. */
const templateDetail = "/agent-templates/kagent/k8s-agent-7f3a91c";

/**
 * The asterisk on a field the form will not submit without.
 *
 * antd draws it from `required` on a `Form.Item`, and every authoring surface here
 * gates its own submit in code — `draftProblems`, `modelDraftIssues`,
 * `validateMcpServerForm` — rather than through antd's rules. So the mark and the
 * gate are two separate statements about the same field, and nothing but a test
 * keeps them agreeing. They had already come apart: the whole agent-template form
 * carried no mark at all while refusing to save without a model configuration, and
 * the model form's API key was required to create with nothing on screen to say so.
 *
 * Asserted as a pair each time — what is marked *and* what is not. A test that only
 * checked the marks would pass just as well on a form that marked every field, which
 * tells a reader nothing about which ones matter.
 */

/** One field's label, by the text a reader sees on it. */
function fieldLabel(page: Page, text: string): Locator {
  // Exact, because these labels are prefixes of each other: "Name" would otherwise
  // match "Namespace" too, and match it first.
  return page
    .locator(".ant-form-item-label label")
    .filter({ has: page.getByText(text, { exact: true }) });
}

/**
 * The labels that carry the mark, and the ones that must not.
 *
 * Both lists in one helper so a call site reads as the claim it is making.
 */
async function expectRequired(
  page: Page,
  { marked, unmarked }: { marked: string[]; unmarked: string[] },
): Promise<void> {
  for (const text of marked) {
    const label = fieldLabel(page, text);
    await expect(label, `"${text}" is required, so it must be marked`).toHaveCount(1);
    await expect(label).toHaveClass(/ant-form-item-required/);
  }
  for (const text of unmarked) {
    const label = fieldLabel(page, text);
    await expect(label, `"${text}" is optional, so it must not be marked`).toHaveCount(1);
    await expect(label).not.toHaveClass(/ant-form-item-required/);
  }
}

test("forms: a field the form refuses to submit without is marked required", async ({
  page,
}) => {
  await test.step("1. the agent template form", async () => {
    await loadPage(page, routes.agentTemplateNew, { title: "New agent template" });
    await expectSettled(page);

    // Name and the model configuration are what `draftProblems` refuses to submit
    // without. Everything else on this form is genuinely optional — including the
    // system prompt, which a template may take from its harness instead.
    await expectRequired(page, {
      marked: ["Name", "Model configuration"],
      unmarked: ["Description", "System prompt"],
    });
  });

  await test.step("2. the model form, where the API key is required only to create", async () => {
    await loadPage(page, routes.modelNew, { title: "New model" });
    await expectSettled(page);

    await expectRequired(page, {
      marked: ["Provider", "Model", "Name", "Namespace", "API key"],
      // A radio group that arrives with a choice already made cannot be missing one.
      unmarked: ["Authentication"],
    });
  });

  await test.step("3. and not on an edit, which keeps the key it already has", async () => {
    await loadPage(page, `${modelEdit}?mock=ok`);
    await expectSettled(page);

    // The fixture holds its credential in a Secret, so the inline-key field is not
    // on screen until that is what the reader is choosing.
    await page
      .getByTestId("model-auth-type")
      .getByText("API key", { exact: true })
      .click();

    // The label says the same thing in words; the mark has to agree with it, or the
    // form is asking for a credential the cluster already holds.
    await expectRequired(page, {
      marked: ["Name", "Namespace"],
      unmarked: ["API key (leave blank to keep existing)"],
    });
  });

  await test.step("4. the harness form", async () => {
    await loadPage(page, routes.harnessNew, { title: "New harness" });
    await expectSettled(page);

    await expectRequired(page, {
      marked: [
        "Namespace",
        "Name",
        "Runtime adapter",
        "Workload image",
        "Worker pool",
        "Snapshot location",
      ],
      // Optional in the CRD's sense and warned about on screen instead: a harness
      // with no selector is created and admits nothing.
      unmarked: ["Admits agent templates labelled"],
    });
  });

  await test.step("5. the MCP server form, whose namespace really is optional", async () => {
    await loadPage(page, routes.mcpServerNew, { title: "New MCP server" });
    await expectSettled(page);

    // Unlike every other form here: `validateMcpServerForm` accepts a blank
    // namespace and the controller defaults it, so a mark would be a lie.
    await expectRequired(page, {
      marked: ["Name", "Server URL"],
      unmarked: ["Namespace", "TLS", "Headers"],
    });
  });

  await test.step("6. the prompt library form", async () => {
    await loadPage(page, routes.promptNew, { title: "New prompt library" });
    await expectSettled(page);

    await expectRequired(page, { marked: ["Namespace", "Name"], unmarked: [] });
  });
});

/**
 * Read-only is not a form, so it asks for nothing.
 *
 * The template details page renders the same component with `readOnly`, and an
 * asterisk there would be asking a reader to supply something they are only looking
 * at — on a template that already has it.
 */
test("forms: the read-only template view marks nothing as required", async ({ page }) => {
  await loadPage(page, templateDetail);
  await expectSettled(page);

  await expect(page.locator(".ant-form-item-label label").first()).toBeVisible();
  await expect(page.locator(".ant-form-item-label label.ant-form-item-required")).toHaveCount(
    0,
  );
});
