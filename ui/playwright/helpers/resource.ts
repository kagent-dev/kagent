/**
 * The moves every resource journey makes.
 *
 * Each list page in this app is the same shape — a filter bar, a Refresh that
 * confirms, a create button, a pencil per row and a `DeleteResourceButton` that asks
 * first — so the knowledge about antd those need lives here once rather than in six
 * specs.
 *
 * Only the *driving* lives here. What each page should then say is the spec's own
 * claim, because a helper that asserted the outcome too would make six specs agree
 * with each other rather than with six pages.
 */

import { expect, type Locator, type Page } from "@playwright/test";

import { withScenario } from "./app";

/**
 * How long one resource's whole lifecycle is allowed to take.
 *
 * Applied per file with `test.describe.configure({ timeout: LIFECYCLE_TIMEOUT })`
 * rather than across the suite: the thirty-second default is load-bearing for the
 * single journeys, where a mock-backed page that needs longer is stuck rather than
 * merely long. A lifecycle is a dozen steps and four round trips through forms, so
 * the default says nothing about its health there — only about its length.
 *
 * **By shape, not by folder.** Any one test of ten or more steps needs it, wherever it
 * lives — `conventions.test.ts` checks that. Applying it to the resource folders alone
 * left `chat/questions.spec.ts` with twelve steps on the default, which is what timed
 * out in CI at step eight while passing twenty times in isolation.
 *
 * Sixty rather than ninety, and the difference is the point: loose enough for a
 * contended run, tight enough that a journey which doubles in cost is still a
 * failure. The slowest is prompts at about forty seconds under full parallel load,
 * its list fanning out one call per namespace.
 *
 * **Live is a different budget, not a slower version of the same one.** Against a
 * cluster every step is a round trip to an API server and some of them wait on a
 * controller to reconcile, which is work no mock does at all — the config already
 * doubles the per-test default for that reason, and a `describe.configure` overrides
 * it, so a lifecycle asking for sixty here would have been the tightest budget in the
 * live run rather than the loosest.
 */
export const LIFECYCLE_TIMEOUT =
  process.env.UI_LOOP_LIVE === "true" ? 180_000 : 60_000;

/**
 * Presses a dialog's button, once the dialog has stopped arriving.
 *
 * antd animates a modal and a popconfirm in, and Playwright can compute a click's
 * coordinates while one is still moving — measured once on a loaded Firefox run at
 * 238px left and 224px below the button, on the backdrop, with the button itself in
 * the same place before the click and after it. It fails as "the dialog would not
 * close" rather than as a missed click, which is why it costs an afternoon each time.
 *
 * Playwright's own stability check compares two animation frames, and a starved main
 * thread can serve both from the same frame of the animation, which reads as
 * stillness. A clock cannot be fooled that way — the animation is over in 200ms
 * whatever else the machine is doing — so this samples on one.
 *
 * This is the one to reach for. `pressUntil` below retries instead, which needs an
 * argument for why a second click is safe.
 */
export async function pressOnce(button: Locator): Promise<void> {
  // Visible first, and `box !== null` below: an element with no box measures `null`,
  // and two of those compare equal — so a button inside a closed dialog would pass the
  // wait on its first pair and fall through to the click this exists to protect.
  await expect(button).toBeVisible();
  let previous: string | undefined;
  await expect(async () => {
    const box = await button.boundingBox();
    const where = JSON.stringify(box);
    const held = box !== null && where === previous;
    previous = where;
    expect(held, `still arriving, at ${where}`).toBe(true);
  }).toPass({ timeout: 10_000, intervals: [100] });
  await button.click();
}

/**
 * Presses a control until the page has acted on it.
 *
 * For a press that can be lost for reasons waiting does not cover — a menu item
 * reached through a dropdown, a dialog opened from inside another. `settled` is the
 * caller's own proof that it landed: the row has gone, the address has changed, the
 * dialog is down.
 *
 * **Three preconditions, each of which has been broken at least once.**
 *
 * - **`button` must not toggle.** Retrying a control that toggles undoes the press
 *   that worked — an antd popconfirm trigger is one.
 * - **`settled` must not be satisfiable by doing nothing.** "The dialog is gone" is
 *   already true before the dialog opens, so the helper returns without pressing
 *   anything and the failure surfaces somewhere else entirely.
 * - **A second click must be harmless where it *lands*,** not only on the button it
 *   was aimed at. A dialog sits over the page, so a retry going out after the dialog
 *   has closed hits whatever is behind it: a popconfirm sits directly over its own
 *   trigger, and a modal usually sits over a list of links.
 *
 * Where any of those is in doubt, use `pressOnce`. What this also gives up is a
 * control that genuinely needed pressing twice — a real product bug it can no longer
 * catch, and the first place to look if a "click does nothing the first time" report
 * arrives.
 */
export async function pressUntil(
  button: Locator,
  settled: () => Promise<unknown>,
  // Half the thirty-second test budget, so that when this is what failed, this is
  // what says so: at thirty the test expired first and reported its own timeout.
  timeout = 15_000,
): Promise<void> {
  await expect(async () => {
    // Bounded, because `toPass` checks its deadline between attempts and no
    // `actionTimeout` is configured: a click blocked by an overlay would otherwise hang
    // inside one attempt until the test budget, which is the failure this reports on.
    if (await button.isVisible()) await button.click({ timeout: 5_000 });
    await settled();
  }).toPass({ timeout });
}

/**
 * Whether something turned up, for a cleanup that has to tell "gone" from "not yet".
 *
 * A `count()` taken straight after a navigation asks the wrong question: `goto` and
 * `loadApp` both return before the page's own read has landed, so nothing is on screen
 * yet and every "is it still there?" is answered no — which is how a cleanup came to
 * skip its delete and leave a real resource on the cluster. Waiting first is what makes
 * an absence mean something.
 *
 * It resolves `false` rather than throwing, because this is called from a `finally`:
 * an assertion failing there would replace the failure the test was actually reporting.
 */
export async function appeared(locator: Locator, timeout = 60_000): Promise<boolean> {
  try {
    await locator.waitFor({ state: "visible", timeout });
    return true;
  } catch (error) {
    /*
     * A timeout means it is not there. Anything else — a locator matching several, say —
     * is a broken check reaching the caller as the same `false`, which a cleanup reads as
     * "nothing to remove" while the resource stays on the cluster. Still `false`, since
     * throwing from a `finally` would replace the test's own failure, but not silently.
     */
    if ((error as Error | undefined)?.name !== "TimeoutError") {
      console.warn(`appeared() could not check this locator: ${String(error)}`);
    }
    return false;
  }
}

/**
 * Shows that a list says it is loading, and hands back a responsive backend.
 *
 * `?mock=slow` delays every call by 2.5 seconds, which is what makes the loading
 * state observable — and it persists for the browsing session, charging that delay to
 * every request in every step after this one. So this returns to `ok` before handing
 * back.
 *
 * The claim being made is that a list says it is loading rather than sitting there
 * looking empty; every step after this reads the same list on a responsive backend
 * anyway.
 */
export async function expectLoading(page: Page, path: string): Promise<void> {
  await page.goto(withScenario(path, "slow"));
  await expect(page.locator(".ant-spin-spinning")).toBeVisible();
  await page.goto(withScenario(path, "ok"));
}

/**
 * Clicks Refresh once it is actually clickable.
 *
 * The control carries the list's own loading state, and antd ignores a click on a
 * loading button — so clicking too early refreshes nothing and the missing
 * confirmation looks like a missing feature. Waiting for spinners to clear is not
 * enough: straight after a navigation there are none yet, so that check passes before
 * the page has mounted.
 */
export async function clickRefresh(page: Page): Promise<void> {
  const button = page.getByTestId("refresh-button");
  await expect(button).toBeEnabled();
  await expect(button).not.toHaveClass(/ant-btn-loading/);
  await button.click();
}

/**
 * The dropdown an antd Select or AutoComplete opens, and the one option in it.
 *
 * rc-select renders a second, invisible `role="listbox"` for screen readers, and
 * `getByRole("option")` resolves to that one and then waits out its timeout for a
 * visibility that never arrives. `.ant-select-item-option` is the row a person clicks.
 */
export function optionNamed(page: Page, label?: string): Locator {
  // Matched on `title`, not on text: antd sets the attribute from the option's label
  // while the rendered text may carry more — a namespace prefix, an icon's alt.
  return page.locator(
    label === undefined
      ? ".ant-select-item-option"
      : `.ant-select-item-option[title="${label}"]`,
  );
}

/**
 * The dropdown on screen, whichever select opened it. By Playwright's own visibility
 * rather than antd's `ant-select-dropdown-hidden`, which a dismissed dropdown does not
 * carry until its close animation ends — lagging at exactly the wrong moment.
 */
function openDropdown(page: Page): Locator {
  return page.locator(".ant-select-dropdown").filter({ visible: true });
}

/** Opens a Select by its test id and picks one option by the label a reader sees. */
export async function selectOption(
  page: Page,
  testId: string,
  label: string,
): Promise<void> {
  await page.getByTestId(testId).click();
  await optionNamed(page, label).click();
}

/**
 * The same, where any option will do — a required field the assertion does not care
 * about, or a model on a cluster whose models the spec did not install.
 *
 * It cannot name what it wants, so it has to be sure *which* dropdown it reads: it
 * waits out any still animating away, then scopes to the one on screen. A live run
 * spent its whole budget clicking an `ate-system` option from a dismissed select.
 */
export async function selectFirstOption(page: Page, testId: string): Promise<void> {
  await expect(openDropdown(page)).toHaveCount(0);
  await page.getByTestId(testId).click();

  const option = openDropdown(page).locator(".ant-select-item-option").first();
  await expect(option, `the select "${testId}" offered no options`).toBeVisible({
    timeout: 30_000,
  });
  await option.click();
}

/**
 * Opens a filter bar's popup and ticks one option.
 *
 * A filter is multi-select, so unlike `selectOption` the popup stays open over the
 * pill row the caller is about to assert on — hence the confirmation and the Escape.
 */
export async function chooseFilter(
  page: Page,
  filterTestId: string,
  label: string,
): Promise<void> {
  await page.getByTestId(filterTestId).click();
  const chosen = optionNamed(page, label);
  await chosen.click();
  // Confirmed where it happened: a click on a popup that has moved or closed under it
  // selects nothing, silently, and is reported much later as a pill that never came.
  await expect(chosen).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Escape");
}

/**
 * The confirmation a row's delete opens, and the modal a page-level one opens.
 *
 * Scoped to the visible one: every row's popconfirm is in the DOM at once, so an
 * unscoped "Delete" can answer a prompt nobody is looking at — and pass while
 * deleting the wrong resource.
 */
export function confirmation(page: Page): Locator {
  return page.locator(".ant-popconfirm:visible");
}

export function dialog(page: Page): Locator {
  return page.locator(".ant-modal:visible");
}

/** Whether any modal is still on screen, overlay included. */
export function anyDialog(page: Page): Locator {
  return page.locator(".ant-modal-wrap");
}

/**
 * Opens one row's delete confirmation and answers it.
 *
 * Any previous confirmation is waited out first, because a dismissed popconfirm stays
 * visible while it animates away — a caller that has just pressed "Keep" would
 * otherwise find that closing dialog and click the Delete inside it.
 *
 * The trigger is clicked exactly once: it is a toggle, so a retry closes what the
 * first press opened. The `toBeVisible` between the two is what makes a Delete aimed
 * at nothing fail here rather than several assertions downstream.
 */
export async function confirmDelete(page: Page, name: string): Promise<void> {
  const open = confirmation(page);
  await expect(open).toHaveCount(0);

  await page.getByTestId(`delete-${name}`).click();
  await expect(open).toBeVisible();
  await pressOnce(open.getByRole("button", { name: "Delete" }));
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
 * `validateMcpServerForm`). So the mark and the gate are two separate statements
 * about the same field, and nothing but a test keeps them agreeing.
 *
 * Both lists are taken so a call site reads as the claim it is making: a check of the
 * marks alone would pass just as well on a form that marked every field.
 */
export async function expectRequired(
  page: Page,
  { marked, unmarked }: { marked: string[]; unmarked: string[] },
): Promise<void> {
  // Both empty is a call that asserts nothing, which is how an edit step kept passing
  // over a form that marks two fields.
  expect(
    marked.length + unmarked.length,
    "expectRequired needs at least one field to be a claim",
  ).toBeGreaterThan(0);

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
