import { test, expect } from "../fixtures/test";
import { loadPage, expectPageTitle, routes } from "../helpers/app";
import { expectShell, navLabels } from "../helpers/nav";

/**
 * App shell — the chrome that every in-app route renders inside.
 *
 * One journey: the shell renders, it advertises every destination the app has, it
 * survives a route change rather than remounting the page whole, and creation is reachable
 * from the lists rather than from the chrome.
 *
 * That last part used to be a Create menu in the header, and the assertion that it is
 * *gone* matters as much as the ones that replace it. The header belongs to the default
 * shell, so a distribution supplying its own layout inherited none of it — a create route
 * reachable only from the header was, there, reachable only by typing the URL.
 */

test("app shell: chrome, navigation entries, and where creation lives", async ({
  page,
}) => {
  await test.step("1. the shell renders around the agents list", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectShell(page);
    // The logo is the wordmark, so there is no text to read — its accessible name
    // is what identifies the product now, and asserting that is also the check
    // that the SVG did not arrive unlabelled.
    const logo = page.getByTestId("app-logo");
    await expect(logo).toHaveAttribute("aria-label", /kagent/i);
    await expect(logo.locator("svg")).toBeVisible();
  });

  await test.step("2. every destination the app ships is in the sidebar", async () => {
    for (const [key, label] of Object.entries(navLabels)) {
      const entry = page.getByTestId(`nav-${key}`);
      await expect(entry, `sidebar is missing "${label}"`).toBeVisible();
      await expect(entry).toContainText(label);
    }
  });

  await test.step("3. the chrome persists across a route change", async () => {
    const headerId = await page
      .getByTestId("app-header")
      .evaluate((node) => {
        // Tag the live node so we can tell "still the same element" from
        // "re-rendered from scratch" after navigating.
        node.setAttribute("data-shell-probe", "1");
        return node.getAttribute("data-shell-probe");
      });
    expect(headerId).toBe("1");

    await page.getByTestId("nav-models").click();
    await page.waitForURL(/\/models(\?|$)/);
    await expectPageTitle(page, "Models");

    // The header element was never torn down, so the shell is genuinely
    // persistent rather than re-mounted per route.
    await expect(page.locator('[data-testid="app-header"][data-shell-probe="1"]')).toHaveCount(1);
  });

  await test.step("4. the chrome offers no create menu of its own", async () => {
    await expect(page.getByTestId("create-menu-trigger")).toHaveCount(0);
  });

  await test.step("5. every list that can create one says so, and reaches its form", async () => {
    // Each list page, its own control, and the form it lands on. Driven page by page
    // because that is the claim: not "a create route exists" but "the list you are
    // looking at offers it".
    const creates = [
      /* The agents list creates a *template*, not an agent. There is no agent to
         create — it is what exists once a harness admits a template — so the control
         that used to say "New agent" now leads to the template form. */
      {
        route: routes.agents,
        title: "Agents",
        testId: "agents-new-template",
        form: "New agent template",
      },
      { route: routes.models, title: "Models", testId: "models-new", form: "New model" },
      {
        route: routes.mcpServers,
        title: "MCP servers",
        testId: "mcp-servers-new",
        form: "New MCP server",
      },
      {
        route: routes.prompts,
        title: "Prompts",
        testId: "prompts-new",
        form: "New prompt library",
      },
    ] as const;

    for (const { route, title, testId, form } of creates) {
      await loadPage(page, route, { title });
      const create = page.getByTestId(testId);
      await expect(create, `${title} has no create control`).toBeVisible();
      await create.click();
      await expectPageTitle(page, form);
      await expectShell(page);
    }
  });
});

/**
 * Navigation is made of links, so it can be opened the way links can.
 *
 * These were menu items with a click handler, which gave a reader nothing to
 * cmd-click: no `href`, so no "open in new tab", no middle-click, no copy-link. That
 * is a reasonable thing to want from navigation — comparing two pages side by side —
 * and the fix is that each entry is a router `Link` rather than a handler.
 *
 * Both halves are asserted, because either alone is a plausible mistake: a plain
 * `<a>` would open a new tab and also reload the whole app on an ordinary click, and a
 * handler with no anchor keeps the app fast while making a new tab impossible. So this
 * checks the anchor is real *and* that an ordinary click never reloads the document.
 */
test("app shell: nav entries are links, not click handlers", async ({ page }) => {
  await loadPage(page, routes.dashboard);

  // Waited for explicitly: `evaluateAll` has no auto-wait, so without this it reads an
  // empty list before the shell has rendered and passes or fails on timing.
  await expect(page.getByTestId("nav-models").locator("a")).toBeVisible();

  // Every entry carries a real destination.
  const hrefs = await page
    .locator('[data-testid^="nav-"] a')
    .evaluateAll((anchors) => anchors.map((a) => a.getAttribute("href")));
  expect(hrefs.length).toBeGreaterThan(3);
  expect(hrefs.every((href) => typeof href === "string" && href.startsWith("/"))).toBe(true);

  // An ordinary click is handled by the router: the document is never reloaded, which a
  // value set on `window` before the click is enough to prove.
  let documentLoads = 0;
  page.on("load", () => { documentLoads += 1; });
  await page.evaluate(() => { (window as unknown as Record<string, string>).spaMarker = "alive"; });

  await page.getByTestId("nav-models").locator("a").click();
  await expect(page).toHaveURL(/\/models$/);
  expect(documentLoads).toBe(0);
  expect(
    await page.evaluate(() => (window as unknown as Record<string, string>).spaMarker),
  ).toBe("alive");

  /*
   * And the entry is a link the browser can act on, rather than a handler dressed as
   * one. Asserted through the anchor, not by modifier-clicking it.
   *
   * Driving a real cmd/ctrl-click turned out to assert the *browser*, not this app:
   * the modifier differs by platform, and headless Chromium on Linux does not raise a
   * new page for it at all, so the same correct markup passed on one engine and timed
   * out on the other. What this app owns is that the destination is a genuine `href`
   * on an `<a>` that is not target-hijacked — given that, opening a new tab is the
   * browser's business and it does it.
   */
  const anchor = page.getByTestId("nav-prompts").locator("a");
  await expect(anchor).toHaveAttribute("href", "/prompts");
  expect(await anchor.evaluate((a) => (a as HTMLAnchorElement).target)).toBe("");
  await expect(page).toHaveURL(/\/models$/);
});

/**
 * The three controls at the foot of the sidebar, and the state two of them keep.
 *
 * Each is a place a reader can get stranded, which is why they are asserted rather
 * than left to the chrome journey above. A theme that forgets itself on reload is
 * worse than no toggle at all; a sidebar that collapses and cannot be reopened loses
 * the whole navigation; and a docs link is the one control here whose destination is
 * not this app, so nothing else would notice if it pointed at the wrong place.
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
