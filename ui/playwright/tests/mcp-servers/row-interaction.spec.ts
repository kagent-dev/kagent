import { test, expect } from "../../fixtures/test";
import { background, paint, settledPaint } from "../../helpers/style";

/**
 * One row, one affordance.
 *
 * The whole row expands a server's tools, and the plus/minus is inside that row — so the
 * two must not respond differently. They did: the control brought antd's own hover and
 * press treatment, which meant the smaller of two overlapping targets for the same action
 * was the one that lit up under the mouse.
 *
 * The press state is asserted with a real mouse-down rather than a class, because `:active`
 * cannot be faked and it is the state that was missing altogether — antd ships a row hover
 * and nothing for the click, so on a slow route a click looked like it had not registered.
 */

const MCP = "/mcp";

test("mcp servers: the row and its expand control behave as one", async ({ page }) => {
  await page.goto(MCP);
  await expect(page.getByTestId("mcp-servers-table")).toBeVisible();

  const row = page.locator("tr.clickable-table-row").first();
  const icon = row.locator(".ant-table-row-expand-icon");
  await expect(icon).toBeVisible();

  await test.step("1. hovering the control adds nothing of its own", async () => {
    const atRest = await paint(icon);
    await icon.hover();
    // Settled before comparing, not read in the same tick: a colour that has not
    // started moving yet matches the colour it started from, so an unsettled read
    // would report "nothing changed" about a state it never saw. See `helpers/style`.
    expect(await settledPaint(icon)).toEqual(atRest);
  });

  await test.step("2. pressing it adds nothing of its own either", async () => {
    const atRest = await paint(icon);
    const box = await icon.boundingBox();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    try {
      const pressed = await settledPaint(icon);
      expect(pressed.background).toBe(atRest.background);
      expect(pressed.shadow).toBe(atRest.shadow);
    } finally {
      // Released even on a failure: a button left held poisons every step after it.
      await page.mouse.up();
    }
  });

  await test.step("3. the row itself does respond to being pressed", async () => {
    const cell = row.locator("td").first();
    const before = await background(cell);

    const box = await cell.boundingBox();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    try {
      // Polled rather than sampled once. The cell transitions over 200ms, so the
      // first read is reliably still the colour it is leaving — this assertion used
      // to pass only because that colour differed from the *hover* one it was being
      // compared against, and it failed outright under a full parallel run.
      await expect
        .poll(() => background(cell), { message: "a pressed row must look pressed" })
        .not.toBe(before);
    } finally {
      await page.mouse.up();
    }
  });

  await test.step("4. and clicking anywhere on it still expands the server", async () => {
    await row.locator("td").first().click();
    await expect(page.locator(".ant-table-expanded-row")).toBeVisible();
  });
});
