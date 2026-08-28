import { test, expect } from "../../fixtures/test";
import {
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
  withScenario,
} from "../../helpers/app";

/**
 * Prompt libraries — the reading journey, list through to detail.
 *
 * Listed in `DEFERRED.md` as blocked on pages that did not exist. They do, and did
 * before this spec; the entry was stale.
 *
 * Both pages are covered in one journey because the interesting part is the step
 * between them: a library's fragments are not on the list at all, so the detail page
 * is the only place the include syntax a reader has to copy is ever shown.
 */

const LIBRARIES = ["shared-fragments", "incident-playbooks"];

test("prompt libraries: the list loads and each library opens its fragments", async ({
  page,
}) => {
  await test.step("1. a loading state precedes the data", async () => {
    await page.goto(withScenario(routes.prompts, "slow"));
    await expect(page.locator(".ant-spin-spinning")).toBeVisible();
  });

  await test.step("2. every library is listed with its key count", async () => {
    // A longer wait than the default, and for a real reason rather than flake:
    // listing across namespaces is a fan-out, because the API requires a namespace
    // and offers no wildcard (see `usePrompts`). Under the slow scenario that is one
    // delayed namespaces call followed by one per namespace — about twice the default
    // expect timeout on this fixture set, where a single call used to be well inside it.
    for (const name of LIBRARIES) {
      await expect(rowNamed(page, name), `"${name}" is missing`).toHaveCount(1, {
        timeout: 30_000,
      });
    }
    await expect(dataRows(page)).toHaveCount(LIBRARIES.length);

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
  });

  await test.step("4. the detail page names the library it is showing", async () => {
    await expect(page.getByTestId("prompt-detail-meta")).toContainText("kagent");
  });

  await test.step("5. an empty result says so instead of showing a bare table", async () => {
    await loadPage(page, routes.prompts, { scenario: "empty", title: "Prompts" });
    await expect(page.getByText("No prompt libraries yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });
});
