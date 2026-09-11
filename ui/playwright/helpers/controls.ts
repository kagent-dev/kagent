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
  if (await control.isChecked()) return;
  await control.click();
  await expect(control).toBeChecked();
}
