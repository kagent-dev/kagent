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
