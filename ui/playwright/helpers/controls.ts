import { expect, type Locator } from "@playwright/test";

/**
 * Ticks a checkbox or radio and waits for the tick, rather than for the click.
 *
 * Not `locator.check()`: it clicks and then reads `checked` straight away, and antd's
 * controls are React-controlled, so the tick arrives on a later commit — around 200ms
 * behind the click when the card holding it re-renders. `check()` reads too early,
 * decides the click was lost, and clicks again, which toggles the control back off.
 *
 * An auto-retrying assertion instead, which re-resolves the locator each poll: a
 * deferred commit passes, and a click that truly went nowhere still fails.
 */
export async function tick(control: Locator): Promise<void> {
  /*
   * Waited for first, and that is not ceremony. `isChecked()` waits for the element
   * with no budget of its own, so a control that never arrives hangs until the whole
   * test times out and reports `locator.isChecked: Test timeout exceeded` — which
   * names this helper and says nothing about the control or why it is missing. The
   * assertion fails in seconds instead, and names what it was looking for.
   */
  await expect(control).toBeVisible();
  if (await control.isChecked()) return;
  await control.click();
  await expect(control).toBeChecked();
}
