import { test, expect } from "../../fixtures/test";

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

  const styles = (locator: typeof icon) =>
    locator.evaluate((el) => {
      const cs = getComputedStyle(el);
      return { background: cs.backgroundColor, shadow: cs.boxShadow, colour: cs.color };
    });

  await test.step("1. hovering the control adds nothing of its own", async () => {
    const atRest = await styles(icon);
    await icon.hover();
    expect(await styles(icon)).toEqual(atRest);
  });

  await test.step("2. pressing it adds nothing of its own either", async () => {
    const atRest = await styles(icon);
    const box = await icon.boundingBox();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    const pressed = await styles(icon);
    await page.mouse.up();

    expect(pressed.background).toBe(atRest.background);
    expect(pressed.shadow).toBe(atRest.shadow);
  });

  await test.step("3. the row itself does respond to being pressed", async () => {
    const cell = row.locator("td").first();
    const before = await cell.evaluate((el) => getComputedStyle(el).backgroundColor);

    const box = await cell.boundingBox();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    const during = await cell.evaluate((el) => getComputedStyle(el).backgroundColor);
    await page.mouse.up();

    expect(during, "a pressed row must look pressed").not.toBe(before);
  });

  await test.step("4. and clicking anywhere on it still expands the server", async () => {
    await row.locator("td").first().click();
    await expect(page.locator(".ant-table-expanded-row")).toBeVisible();
  });
});
