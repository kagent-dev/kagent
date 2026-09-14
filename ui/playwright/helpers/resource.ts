/**
 * The moves every resource journey makes.
 *
 * Each list page in this app is the same shape — a filter bar, a Refresh that
 * confirms, a create button, a pencil per row and a `DeleteResourceButton` that asks
 * first — so the CRUD specs would otherwise repeat the same five interactions with
 * five slightly different sets of local knowledge about antd. They are here instead,
 * with the knowledge written down once.
 *
 * Only the *driving* lives here. What each page should then say is the spec's own
 * claim and stays in the spec, because a helper that asserted the outcome too would
 * make six specs agree with each other rather than with six pages.
 */

import { expect, type Locator, type Page } from "@playwright/test";

/**
 * How long one resource's whole lifecycle is allowed to take.
 *
 * Applied per file with `test.describe.configure({ timeout: LIFECYCLE_TIMEOUT })`
 * rather than raised across the suite, because the rest of the suite's tests are
 * single journeys and the thirty-second default is load-bearing there: a mock-backed
 * page that needs longer than that is stuck, and absorbing it into a larger budget
 * would turn a useful failure into a slow pass.
 *
 * A CRUD journey is a different shape. It is a dozen-odd steps with four round trips
 * through forms, and the longest of them takes about thirty seconds on an idle machine
 * — so the default is not a claim about health here, only about length. Ninety leaves
 * roughly three times that, which is the headroom the run needs when two browser
 * engines and two dev servers are competing for one laptop.
 */
export const LIFECYCLE_TIMEOUT = 90_000;

/**
 * Presses a dialog's button until the dialog has acted on it.
 *
 * antd animates a modal and a popconfirm in, and a click that lands while one is still
 * arriving is dropped — often enough on Firefox under a loaded parallel run to be the
 * single largest source of flake in these specs. It fails in the worst possible way: the
 * dialog simply stays up until the assertion times out, which reads as a confirmation
 * that cannot be dismissed rather than as a click that missed.
 *
 * Retrying is safe because every button this is used on is idempotent in the only sense
 * that matters here — `Keep` and `Keep editing` close a dialog, `Delete` deletes a thing
 * that is then gone — and the `isVisible` guard means a button that has already done its
 * job is not pressed again. `settled` is the caller's own proof that it did: the row has
 * gone, the address has changed, the dialog is down.
 *
 * Measured rather than assumed: with this at every dialog in these specs, five
 * consecutive full runs produced no failure in one. Without it, schedules failed three
 * times in five and prompts once.
 */
export async function pressUntil(
  button: Locator,
  settled: () => Promise<unknown>,
  timeout = 30_000,
): Promise<void> {
  await expect(async () => {
    if (await button.isVisible()) await button.click();
    await settled();
  }).toPass({ timeout });
}

/**
 * Clicks Refresh once it is actually clickable.
 *
 * The control carries the list's own loading state, and antd ignores a click on a
 * button that is loading — so clicking too early refreshes nothing and the missing
 * confirmation looks like a missing feature. Waiting for spinners to clear is not
 * enough: straight after a navigation there are no spinners yet, so that check passes
 * before the page has even mounted.
 */
export async function clickRefresh(page: Page): Promise<void> {
  const button = page.getByTestId("refresh-button");
  await expect(button).toBeEnabled();
  await expect(button).not.toHaveClass(/ant-btn-loading/);
  await button.click();
}

/**
 * Opens a filter's popup and ticks one option by the label the reader sees.
 *
 * rc-select renders a *second*, invisible `role="listbox"` for screen readers, and
 * `getByRole("option")` resolves to that one and then waits forever for a visibility
 * that never arrives — reporting the option as absent while it is on screen the whole
 * time. Locating `.ant-select-item-option` by its title is what actually points at the
 * row a person clicks.
 */
export async function chooseFilter(
  page: Page,
  filterTestId: string,
  label: string,
): Promise<void> {
  await page.getByTestId(filterTestId).click();
  const option = page.locator(`.ant-select-item-option[title="${label}"]`);
  await option.click();
  // The click, confirmed where it happened: a click on a popup that has moved or
  // closed under it selects nothing, silently, and is reported much later as a pill
  // that never appeared.
  await expect(option).toHaveAttribute("aria-selected", "true");
  // Otherwise the popup covers the pill row the caller is about to assert on.
  await page.keyboard.press("Escape");
}

/**
 * Opens one row's delete confirmation and answers it.
 *
 * Two pieces of local knowledge, both of which have cost a debugging session:
 *
 * - **Scoped to the visible popconfirm.** Every row's confirmation is in the DOM at
 *   once, so an unscoped "Delete" can answer a prompt nobody is looking at — and pass
 *   while deleting the wrong resource.
 * - **Any previous confirmation is waited out first.** A dismissed popconfirm stays
 *   visible while it animates away, so a caller that has just pressed "Keep" would
 *   otherwise find that closing dialog, click the Delete inside it, and hit an element
 *   that is on its way off the page.
 */
export async function confirmDelete(page: Page, name: string): Promise<void> {
  const open = page.locator(".ant-popconfirm:visible");
  await expect(open).toHaveCount(0);

  await page.getByTestId(`delete-${name}`).click();
  await open.getByRole("button", { name: "Delete" }).click();
}

/** One field's label, by the text a reader sees on it. */
function fieldLabel(page: Page, text: string): Locator {
  // Exact, because these labels are prefixes of each other: "Name" would otherwise
  // match "Namespace" too, and match it first.
  return page
    .locator(".ant-form-item-label label")
    .filter({ has: page.getByText(text, { exact: true }) });
}

/**
 * The asterisk on a field the form will not submit without — and its absence.
 *
 * antd draws the mark from `required` on a `Form.Item`, while every authoring surface
 * here gates its own submit in code (`draftProblems`, `modelDraftIssues`,
 * `validateMcpServerForm`) rather than through antd's rules. So the mark and the gate
 * are two separate statements about the same field, and nothing but a test keeps them
 * agreeing. They had already come apart twice: the whole agent-template form carried
 * no mark while refusing to save without a model configuration, and the model form's
 * API key was required to create with nothing on screen to say so.
 *
 * Both lists are taken, `marked` and `unmarked`, so a call site reads as the claim it
 * is making. A check of the marks alone would pass just as well on a form that marked
 * every field, which tells a reader nothing about which ones matter.
 */
export async function expectRequired(
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
    await expect(label, `"${text}" is optional, so it must not be marked`).toHaveCount(
      1,
    );
    await expect(label).not.toHaveClass(/ant-form-item-required/);
  }
}
