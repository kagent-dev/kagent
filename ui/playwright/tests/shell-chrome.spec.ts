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

test("app shell: collapsed, every nav icon sits on the rail's centre line", async ({
  page,
}) => {
  await page.goto("/agents");
  await expect(page.getByTestId("nav-agents")).toBeVisible();
  await page.getByText("Collapse", { exact: true }).click();

  // Measured rather than eyeballed: the rows carry a left-measured padding for the label
  // they no longer show, which displaced the library's own collapsed centring and left
  // every icon a few pixels to the left — visible as sloppiness, invisible to a
  // screenshot test that only asks whether the rail rendered.
  const offsets = await page.evaluate(() => {
    const rail = document.querySelector('[data-testid="app-sidebar"]')!;
    const box = rail.getBoundingClientRect();
    // Excluding the 1px right border, which is not part of the space icons sit in.
    const centre = box.left + (box.width - 1) / 2;
    return [...document.querySelectorAll('[data-testid^="nav-"]')].map((row) => {
      const icon = row.querySelector("svg")!.getBoundingClientRect();
      return { key: (row as HTMLElement).dataset.testid, off: icon.left + icon.width / 2 - centre };
    });
  });

  expect(offsets.length).toBeGreaterThan(3);
  for (const { key, off } of offsets) {
    /*
     * Two pixels, which is a tolerance rather than a target.
     *
     * An icon of even width in a rail of odd width cannot land dead centre, and the
     * leftover is rounded differently by each engine on each platform — Firefox on
     * Linux lands 1.4px out where Chromium on macOS lands under 1. Neither is visible.
     * The displacement this test exists to catch was the label's padding pushing every
     * icon several pixels left, which 2px still fails on; tightening it further only
     * makes the suite report the renderer rather than the layout.
     */
    expect(Math.abs(off), `${key} is ${off.toFixed(1)}px off the centre line`).toBeLessThanOrEqual(2);
  }
});
