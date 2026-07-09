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
