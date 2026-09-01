import { test, expect } from "../fixtures/test";
import { loadPage, routes } from "../helpers/app";

/**
 * The three controls at the foot of the sidebar, and the state two of them keep.
 *
 * Worth its own spec because each is a place a reader can get stranded. A theme
 * that forgets itself on reload is worse than no toggle at all; a sidebar that
 * collapses and cannot be reopened loses the whole navigation; and a docs link is
 * the one control here whose destination is not this app, so nothing else would
 * notice if it pointed at the wrong place.
 */

test("app shell: the sidebar's footer controls", async ({ page }) => {
  await test.step("1. documentation points at the project's docs", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });

    const docs = page.getByTestId("sidebar-docs");
    await expect(docs).toHaveAttribute("href", "https://kagent.dev/docs/kagent");
    // Opens away from the console, and without handing the target a referrer that
    // names the page — this URL can carry a cluster name.
    await expect(docs).toHaveAttribute("target", "_blank");
    await expect(docs).toHaveAttribute("rel", /noopener/);
  });

  await test.step("2. the theme toggle switches, and says what it will do", async () => {
    const toggle = page.getByTestId("theme-toggle");
    const before = await page.evaluate(() => document.documentElement.dataset.theme);

    // The label names the destination, not the current state: a toggle announced as
    // where it already is tells a screen reader the opposite of what it does.
    await expect(toggle).toHaveAttribute(
      "aria-label",
      before === "dark" ? /light/i : /dark/i,
    );

    await toggle.click();

    const after = before === "dark" ? "light" : "dark";
    await expect
      .poll(() => page.evaluate(() => document.documentElement.dataset.theme))
      .toBe(after);
    // `color-scheme` as well, which is what the browser draws its own scrollbars
    // and form controls from — those are not ours to style.
    await expect
      .poll(() => page.evaluate(() => document.documentElement.style.colorScheme))
      .toBe(after);
  });

  await test.step("3. the choice survives a reload", async () => {
    const chosen = await page.evaluate(() => document.documentElement.dataset.theme);

    await page.reload();
    await expect(page.getByTestId("app-sidebar")).toBeVisible();

    // Remembered, rather than falling back to the system preference — which is the
    // point of writing only an explicit choice down.
    await expect
      .poll(() => page.evaluate(() => document.documentElement.dataset.theme))
      .toBe(chosen);
  });

  await test.step("4. the sidebar collapses to icons and comes back", async () => {
    const sidebar = page.getByTestId("app-sidebar");
    const expandedWidth = (await sidebar.boundingBox())!.width;

    await page.getByTestId("sidebar-collapse").click();

    await expect.poll(async () => (await sidebar.boundingBox())!.width).toBeLessThan(
      expandedWidth,
    );
    // Still navigable: the entries are there as icons, so collapsing hides labels
    // rather than the navigation.
    await expect(page.getByTestId("nav-agents")).toBeVisible();
    await expect(page.getByTestId("sidebar-collapse")).toHaveAttribute(
      "aria-expanded",
      "false",
    );

    await page.getByTestId("sidebar-collapse").click();
    await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(
      expandedWidth,
    );
  });
});

test("app shell: collapsed, every nav icon is centred in its row", async ({
  page,
}) => {
  await page.goto("/agents");
  await expect(page.getByTestId("nav-agents")).toBeVisible();
  await page.getByTestId("sidebar-collapse").click();

  await expect(page.getByTestId("sidebar-collapse")).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect(page.locator(".ant-menu-inline-collapsed")).toHaveCount(1);

  /*
   * Then wait out the collapse, asked of the animations rather than of the clock: the
   * rows' padding is genuinely asymmetric mid-transition, and it settles a frame or
   * two after the width does. Not the fix for what was reported — a settled rail was
   * still out against its own centre line — only the precondition for measuring.
   */
  await page.waitForFunction(() => {
    const rail = document.querySelector('[data-testid="app-sidebar"]')!;
    return rail
      .getAnimations({ subtree: true })
      .every((animation) => animation.playState !== "running");
  });

  // Measured rather than eyeballed: the rows carry a left-measured padding for the label
  // they no longer show, which displaced the library's own collapsed centring and left
  // every icon a few pixels to the left — visible as sloppiness, invisible to a
  // screenshot test that only asks whether the rail rendered.
  const rows = await page.evaluate(() => {
    const rail = document.querySelector('[data-testid="app-sidebar"]')!;
    const railBox = rail.getBoundingClientRect();
    // Excluding the 1px right border, which is not part of the space icons sit in.
    const railCentre = railBox.left + (railBox.width - 1) / 2;
    return [...document.querySelectorAll('[data-testid^="nav-"]')].map((row) => {
      const box = row.getBoundingClientRect();
      const icon = row.querySelector("svg")!.getBoundingClientRect();
      /*
       * The row's whole content, margins included, rather than the icon alone: a
       * collapsed row still holds the hidden label, whose width and margin are a font
       * and engine question. Flex centres the outer boxes of what it is given, so
       * measuring those is the form of the question with no renderer in the answer.
       */
      const children = [...row.children].map((child) => {
        const rect = child.getBoundingClientRect();
        const style = getComputedStyle(child);
        return {
          left: rect.left - Number.parseFloat(style.marginLeft),
          right: rect.right + Number.parseFloat(style.marginRight),
        };
      });
      const contentLeft = Math.min(...children.map((child) => child.left));
      const contentRight = Math.max(...children.map((child) => child.right));
      return {
        key: (row as HTMLElement).dataset.testid,
        iconWidth: icon.width,
        before: contentLeft - box.left,
        after: box.right - contentRight,
        // And where the icon lands against the rail, which is the thing a reader
        // actually sees. Kept loose, for the reason given below.
        offCentre: icon.left + icon.width / 2 - railCentre,
      };
    });
  });

  expect(rows.length).toBeGreaterThan(3);
  for (const { key, iconWidth, before, after, offCentre } of rows) {
    /*
     * The strict half, and the one that catches the defect: the padding the collapsed
     * row must not keep shows up here as the whole padding's worth of asymmetry,
     * against a free space flex splits evenly. Measured against the rail instead there
     * is a leftover nothing can settle — an even-width icon in an odd-width rail,
     * rounded per engine — which is what the 2px tolerance had come to report.
     */
    expect(
      Math.abs(before - after),
      `${key}'s contents sit ${before.toFixed(1)}px from one edge of its row and ` +
        `${after.toFixed(1)}px from the other`,
    ).toBeLessThanOrEqual(1);

    /*
     * The loose half, a different claim: the row itself is where it should be, which
     * nothing above would notice. Half the icon's width, so the tolerance comes from
     * the layout — it says the rail's centre line passes through the icon.
     */
    expect(
      Math.abs(offCentre),
      `${key} is ${offCentre.toFixed(1)}px off the rail's centre line`,
    ).toBeLessThan(iconWidth / 2);
  }
});
