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

import { withScenario } from "./app";

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
 * through forms, so the default is not a claim about health here, only about length.
 *
 * Sixty rather than ninety, and the difference is the point: a budget has to be loose
 * enough for a contended run and tight enough that a journey which doubles in cost is
 * still a failure. The slowest of these is prompts at about forty seconds under full
 * parallel load — its list fans out one call per namespace — so sixty clears the worst
 * observed run with room, where ninety left space for a regression to hide in.
 */
export const LIFECYCLE_TIMEOUT = 60_000;

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
 *
 * **`settled` must not be satisfiable by doing nothing, and `button` must not toggle.**
 * Two preconditions, learned by breaking both at once. Retrying a control that toggles
 * undoes the press that worked — an antd popconfirm trigger is one. And a `settled` like
 * "the dialog is gone" is already true before the dialog opens, so the helper returns
 * without pressing anything and the failure surfaces somewhere else entirely. Give it a
 * button that only acts in one direction, and a proof that is false until it has acted.
 *
 * **And a third, which is the one that keeps being missed: a second click has to be
 * harmless where it *lands*, not only on the button it was aimed at.** A dialog sits over
 * the page, so a retry that goes out after the dialog has closed lands on whatever was
 * underneath — and that is rarely nothing. Measured, by trying it in four more places at
 * once: a popconfirm sits directly over its own trigger, so the stray click reopened what
 * the first one closed and the two took turns until the test timed out, and a modal over a
 * conversation list put the stray click on a row link and navigated the test off the page
 * it was asserting on. Three specs that had been clean for five full runs went to between
 * one failure in six and one in three. Where the button cannot be pressed twice — or the
 * page under it cannot take a stray click — use `pressOnce` below instead.
 *
 * **What it gives up, so that nobody has to rediscover it.** A control that genuinely
 * needed pressing twice would now pass here. That is a real product bug this can no
 * longer catch, and it is the price of the stability above — taken knowingly, because
 * the cause is antd's open animation rather than anything the page does, and because a
 * suite that fails one run in two catches nothing at all. If a "click does nothing the
 * first time" report ever arrives, this is the first place to look, and the assertion
 * that would catch it is a plain `click()` plus `settled` rather than this.
 */
export async function pressUntil(
  button: Locator,
  settled: () => Promise<unknown>,
  /*
   * Half the tight default, so the helper can still say what happened.
   *
   * It was thirty, which is exactly Playwright's own test budget in mock mode — so in
   * every spec that does not raise it, the test expired first and reported its own
   * timeout instead of this one's. That is the failure `tick` was fixed for a few lines
   * below: a message naming the helper and saying nothing about which button, or what
   * the caller was waiting to see. Fifteen is far past any press that is going to land.
   */
  timeout = 15_000,
): Promise<void> {
  await expect(async () => {
    if (await button.isVisible()) await button.click();
    await settled();
  }).toPass({ timeout });
}

/**
 * Presses a dialog button that must not be pressed twice, once the dialog has arrived.
 *
 * `pressUntil` above answers the same failure by pressing again, which needs a button
 * that can be pressed again *and* a page underneath that can take a stray click. Both are
 * rarer than they look: "Create a link" issues a share every time it is answered, and the
 * page under a dialog is usually a list of links. See that helper's third precondition for
 * what pressing again cost when it was tried in four more places at once. So this is the
 * one to reach for when a dialog button is pressed, and `pressUntil` the one that needs an
 * argument for why retrying is safe there.
 *
 * What it waits for is the cause rather than the symptom. antd zooms a modal in from a
 * fifth of its size, and a click aimed at a button inside one lands wherever that button
 * was when the coordinates were taken. Measured once on a loaded Firefox run: the click
 * landed 238px left and 224px below the button, on the backdrop behind it — while the
 * button itself was in the same place before the click and after it. Only during.
 *
 * Playwright already waits for an element to hold still, and repeating that check here is
 * the point rather than an oversight: it compares two animation frames, and a starved main
 * thread can serve both from the same frame of the animation, which reads as stillness. A
 * clock cannot be fooled that way — the animation is over in 200ms whatever else the
 * machine is doing — so this samples on one.
 */
export async function pressOnce(button: Locator): Promise<void> {
  let previous: string | undefined;
  await expect(async () => {
    const where = JSON.stringify(await button.boundingBox());
    const held = where === previous;
    previous = where;
    expect(held, `still arriving, at ${where}`).toBe(true);
  }).toPass({ timeout: 10_000, intervals: [100] });
  await button.click();
}

/**
 * Shows that a list says it is loading, and hands back a responsive backend.
 *
 * The one antd internal the specs still needed, and the reason the lint rule stops short
 * of banning every `.ant-` class: there was nothing to point people at.
 *
 * `?mock=slow` delays every call by 2.5 seconds, which is what makes the loading state
 * observable — and a trap, because the scenario persists for the browsing session. Left
 * in force it charges 2.5 seconds to every request in every step that follows: measured
 * at six seconds on the prompts journey alone, whose list fans out one call per
 * namespace. So this returns to `ok` before handing back.
 *
 * What that gives up is the assertion that *this* slow read resolves into data, and it
 * is worth being clear that the loss is nominal: the claim being made is that a list
 * says it is loading rather than sitting there looking empty, and every step after this
 * one reads the same list on a responsive backend anyway. Waiting the slow read out
 * instead costs the delay twice — once for the read, once for the reset — which is what
 * the obvious alternative was measured doing.
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
 * The dropdown an antd Select or AutoComplete opens, and the one option in it.
 *
 * rc-select renders a *second*, invisible `role="listbox"` for screen readers, and
 * `getByRole("option")` resolves to that one and then waits forever for a visibility
 * that never arrives — reporting the option as absent while it is on screen the whole
 * time. Locating `.ant-select-item-option` is what actually points at the row a person
 * clicks, and keeping that knowledge here is why specs do not have to hold it.
 */
export function optionNamed(page: Page, label?: string): Locator {
  // Matched on `title`, not on text. antd sets the attribute from the option's label
  // while the rendered text may carry more — a namespace prefix, a provider icon's
  // alt — so a text match silently finds nothing and waits out the whole timeout.
  return page.locator(
    label === undefined
      ? ".ant-select-item-option"
      : `.ant-select-item-option[title="${label}"]`,
  );
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
 * The same, where any option will do.
 *
 * For a field a form requires but the assertion does not care about — a namespace on a
 * harness, say, where the point of the step is the image rather than where it lives.
 */
export async function selectFirstOption(page: Page, testId: string): Promise<void> {
  await page.getByTestId(testId).click();
  await optionNamed(page).first().click();
}

/**
 * Opens a filter bar's popup and ticks one option.
 *
 * A filter is multi-select, so unlike `selectOption` the popup stays open over the pill
 * row the caller is about to assert on — hence the confirmation and the Escape.
 */
export async function chooseFilter(
  page: Page,
  filterTestId: string,
  label: string,
): Promise<void> {
  await page.getByTestId(filterTestId).click();
  const chosen = optionNamed(page, label);
  await chosen.click();
  // The click, confirmed where it happened: a click on a popup that has moved or closed
  // under it selects nothing, silently, and is reported much later as a pill that never
  // appeared.
  await expect(chosen).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Escape");
}

/**
 * The confirmation a row's delete opens, and the modal a page-level one opens.
 *
 * Scoped to the visible one, which is the part worth having written down: every row's
 * popconfirm is in the DOM at once, so an unscoped "Delete" can answer a prompt nobody
 * is looking at — and pass while deleting the wrong resource.
 */
export function confirmation(page: Page): Locator {
  return page.locator(".ant-popconfirm:visible");
}

export function dialog(page: Page): Locator {
  return page.locator(".ant-modal:visible");
}

/** Whether any modal is still on screen, overlay included — see `pressUntil`. */
export function anyDialog(page: Page): Locator {
  return page.locator(".ant-modal-wrap");
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
  const open = confirmation(page);
  await expect(open).toHaveCount(0);

  /*
   * Both of these are plain clicks, and that is the finding rather than an oversight.
   *
   * `pressUntil` was tried here and broke four resource specs on Firefox — worse, it
   * broke them silently. A popconfirm's trigger is a toggle, so retrying it closes what
   * the first click opened; and the press after it asked for "the confirmation is gone",
   * which a confirmation that never opened satisfies at once. The helper then clicked
   * nothing, returned happily, and the row was still there three steps later. Restoring
   * the clicks restored the green.
   *
   * The `toBeVisible` between them is the part worth keeping from that attempt: it
   * proves the trigger opened the confirmation, so a Delete aimed at nothing fails here
   * rather than several assertions downstream.
   */
  await page.getByTestId(`delete-${name}`).click();
  await expect(open).toBeVisible();
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
