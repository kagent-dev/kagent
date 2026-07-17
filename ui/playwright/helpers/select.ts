// Driver for Radix <Select> (used by the agent model/type pickers, model form,
// etc.). Options are portaled and only exist once the trigger is opened.

import { expect, type Locator, type Page } from "@playwright/test";

/**
 * Pick an option from a Radix Select.
 * @param trigger the SelectTrigger — a Locator or a selector string (e.g. "#agent-field-model").
 * @param optionName the visible option text (exact or RegExp).
 */
export async function selectOption(
  page: Page,
  trigger: Locator | string,
  optionName: string | RegExp,
): Promise<void> {
  const triggerLocator = typeof trigger === "string" ? page.locator(trigger) : trigger;
  await expect(triggerLocator).toBeEnabled();
  await triggerLocator.click();
  await page.getByRole("option", { name: optionName }).click();
}

/**
 * Pick a namespace from a NamespaceCombobox (Popover + cmdk Command, not a Radix
 * Select). cmdk renders items as role="option"; the accessible name includes the
 * "Status: …" suffix, so match on the namespace name as a substring.
 * @param trigger the combobox trigger — a Locator or selector (e.g. "#agent-field-namespace").
 */
export async function selectNamespace(
  page: Page,
  trigger: Locator | string,
  namespace: string,
): Promise<void> {
  const triggerLocator = typeof trigger === "string" ? page.locator(trigger) : trigger;
  await expect(triggerLocator).toBeEnabled();
  await triggerLocator.click();
  await page.getByRole("option", { name: namespace }).first().click();
}
