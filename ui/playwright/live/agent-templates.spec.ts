import { test, expect } from "../fixtures/test";
import {
  confirmation,
  pressOnce,
  selectFirstOption,
  selectOption,
} from "../helpers/resource";
import {
  dataRows,
  expectNoLoadFailure,
  loadApp,
  rowNamed,
  throwawayName,
} from "../helpers/app";
import { liveRoutes } from "./helpers/live";

/**
 * Creating and deleting an agent template, on a real cluster, through the UI.
 *
 * The property it exists for: **admission is the controller's answer, not the form's.**
 * `admittingHarnesses` is read from the template's *status* and cannot be computed in
 * the browser, so a fixture can return any value it likes and the page will draw it.
 *
 * It replaces `agent-lifecycle.spec.ts`, which drove `/agents/new` — a page removed
 * long before, for an agent nobody creates. Nothing ran the suite, so nothing said so.
 */

/** The one this journey makes and removes. Carries the run, for litter left by a kill. */
const TEMPLATE = throwawayName("template");
const NAMESPACE = "kagent";

test("live: an agent template is created, admitted and deleted through the UI", async ({
  page,
}) => {
  let created = false;
  /** Which harness this install offered, read off the button in step 2. */
  let harness = "";

  try {
    await test.step("1. the form offers the cluster's own model configurations", async () => {
      await loadApp(page, liveRoutes.agentTemplateNew);
      await expectNoLoadFailure(page);

      await selectOption(page, "template-form-namespace", NAMESPACE);
      await page.getByTestId("template-form-name").fill(TEMPLATE);

      // `spec.modelConfig` is the one field the CRD requires, and the options are the
      // cluster's own ModelConfigs. Whichever one this install ships, rather than a
      // name — the assertion is that the cluster answered, not which model it named,
      // and `selectFirstOption` fails with that message if the list is empty.
      await selectFirstOption(page, "template-form-model");
    });

    await test.step("2. and the cluster's own harnesses, one of which makes it usable", async () => {
      /*
       * Two states, and which one appears is a fact about the cluster. With one harness
       * the form applies its labels unasked, there being no decision to make; with
       * several it warns until told. A `setup-cluster` cluster has one, CI's fixture
       * five, so a spec that knew only the second would fail on every laptop.
       */
      const admission = page.getByTestId("template-form-admission");
      const buttons = page.locator('[data-testid^="template-form-admit-"]');
      await expect(buttons.first(), "the cluster offered no harnesses").toBeVisible({
        timeout: 30_000,
      });
      const offered = (await buttons.allTextContents())
        .map((name) => name.trim())
        .filter(Boolean);

      const admissionText = (await admission.textContent()) ?? "";
      harness =
        offered.find((name) => admissionText.includes(`admitted by ${name}`)) ?? "";

      if (harness === "") {
        await expect(admission).toContainText("No harness will run this template");

        // Enabled ones only: the form disables a harness whose selector is empty, since
        // such a harness admits nothing and has no labels to copy. Clicking one would
        // spend the step's budget on a button that was never going to answer.
        const admit = page
          .locator('[data-testid^="template-form-admit-"]:not([disabled])')
          .first();
        await expect(admit, "no harness on the cluster admits anything").toBeVisible();
        harness = ((await admit.textContent()) ?? "").trim();
        // The button applies whatever labels that harness's selector matches on, which
        // is the step a reader is most likely to miss and the one that makes the
        // template mean anything.
        await admit.click();
      }

      expect(harness, "no harness name could be read from the form").not.toBe("");
      await expect(admission).toContainText(`admitted by ${harness}`);
    });

    await test.step("3. submitting reaches the controller and lands on the list", async () => {
      await expect(page.getByTestId("template-submit")).toBeEnabled();
      await page.getByTestId("template-submit").click();

      // Success is leaving the form. A create the controller refused keeps the reader on
      // it with `template-create-error` — which is the shape the defect this suite was
      // written for produced for a template that had in fact been created.
      await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 60_000 });
      created = true;
      await expect(page).toHaveURL(new RegExp(`[?&]ns=${NAMESPACE}(&|$)`));
    });

    await test.step("4. the row is read back from the cluster", async () => {
      await expectNoLoadFailure(page);
      await expect(rowNamed(page, TEMPLATE)).toHaveCount(1, { timeout: 60_000 });
      /*
       * Deliberately not asserting the harness here. The row carries the namespace too,
       * and on every cluster this runs against both are `kagent` — so the assertion
       * would pass on the namespace whatever the admission column said. It belongs on an
       * element holding the harness and nothing else, which is the next step.
       */
    });

    await test.step("5. the controller agrees a harness admits it", async () => {
      await page.getByTestId(`template-link-${TEMPLATE}`).click();
      await page.waitForURL(
        new RegExp(`/agent-templates/${NAMESPACE}/${TEMPLATE}`),
        { timeout: 60_000 },
      );
      // The claim this journey exists for, on an element that holds "Runs on" and the
      // admitting harnesses and nothing else. `admittingHarnesses` comes from the
      // template's *status*, so the harness appearing here means the controller observed
      // the labels the form applied and agreed — not that the form echoed itself back.
      await expect(page.getByTestId("template-admission-status")).toContainText(harness, {
        timeout: 60_000,
      });
      await expect(page.getByTestId("template-admission-status")).not.toContainText(
        "No harness",
      );
    });

    await test.step("6. deleting says what it costs, against the cluster's own count", async () => {
      const remove = page.getByTestId(`delete-${TEMPLATE}`);
      await expect(remove).toContainText("Delete template");
      await remove.click();

      /*
       * Either branch is legitimate: the count comes from `status.harnesses` while step 5
       * waited on `admittingHarnesses`, two fields filled by different work. The mock
       * suite pins the wording; what a cluster shows is that the sentence is computed
       * from real state at all, rather than coming back blank.
       */
      await expect(page.getByTestId("template-delete-consequence")).toContainText(
        /built from this template|no agent was ever built from it/,
        { timeout: 30_000 },
      );
    });

    await test.step("7. confirming removes it, and the re-read list agrees", async () => {
      // Scoped to the visible popconfirm, and pressed once it has stopped arriving —
      // see `helpers/resource` for what each of those is protecting against.
      await pressOnce(confirmation(page).getByRole("button", { name: "Delete" }));
      await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 60_000 });
      created = false;

      await expectNoLoadFailure(page);
      // The rest of the list is still there — the cluster installs templates of its own
      // — so "gone" names that one template rather than a read that returned nothing.
      // That distinction is the whole reason `expectNoLoadFailure` exists, and an empty
      // table is exactly how a failed list would look.
      await expect(dataRows(page).first()).toBeVisible({ timeout: 60_000 });
      await expect(rowNamed(page, TEMPLATE)).toHaveCount(0, { timeout: 60_000 });
    });
  } finally {
    /*
     * A real resource on a real cluster, so a run that dies midway takes it with it.
     * Through the UI because the app speaks gRPC-Web — there is no REST endpoint to
     * call, though the previous version of this file believed there was.
     */
    if (created) {
      await page.goto(`/agent-templates/${NAMESPACE}/${TEMPLATE}`);
      await page.getByTestId(`delete-${TEMPLATE}`).click();
      await pressOnce(confirmation(page).getByRole("button", { name: "Delete" }));
      await page.waitForURL(/\/agents\?.*tab=templates/, { timeout: 60_000 });
    }
  }
});
