import { test, expect } from "../fixtures/test";

/**
 * Text the brand colour is painted with has to be readable on the page it sits on.
 *
 * Three separate places got this wrong the same way: `primary` is a deep purple chosen as
 * a *fill* with light text on it, and used as ink on the dark theme's near-black page it
 * measured 2.2:1 to 2.5:1 where small text needs 4.5. Each was found by measuring rather
 * than looking — which is the point of this spec: a purple-on-near-black link looks
 * deliberate in a screenshot, and a reviewer flicking between themes will not catch it.
 *
 * The ratios are computed the way the eye sees them, compositing every translucent layer
 * down to an opaque colour. Reading the first non-transparent background and treating it
 * as opaque reports a *tinted* panel as a solid brand fill and fails a colour that is
 * fine — which happened while investigating this, and cost a wrong conclusion.
 */

const AA_SMALL_TEXT = 4.5;
/** What WCAG asks of a control's own edges and state, rather than of its text. */
const AA_NON_TEXT = 3;

/**
 * The first colour in a CSS value — a box-shadow's colour rather than its offsets.
 *
 * With its alpha, which is the whole point: antd's elevation shadow is white at one
 * percent, and read as opaque white it measures as the most visible edge on the page
 * instead of the invisible one it is. That is the mistake the note above this file
 * records against the *page* colours, made a second time against a shadow.
 */
function colourOf(value: string): { rgb: number[]; a: number } {
  const parts = value.match(/rgba?\(([^)]+)\)/)?.[1].split(",").map(Number) ?? [
    0, 0, 0, 1,
  ];
  return { rgb: parts.slice(0, 3), a: parts.length > 3 ? parts[3] : 1 };
}

/** A translucent colour flattened onto what is behind it. */
function over(fg: { rgb: number[]; a: number }, bg: number[]): number[] {
  return fg.rgb.map((channel, index) => channel * fg.a + bg[index] * (1 - fg.a));
}

function contrast(a: number[], b: number[]): number {
  const luminance = (rgb: number[]) => {
    const [r, g, blue] = rgb.map((channel) => {
      const s = channel / 255;
      return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * blue;
  };
  const [lighter, darker] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (lighter + 0.05) / (darker + 0.05);
}

/** Contrast of an element's text against everything painted behind it. */
const PROBE = () => {
  const parse = (value: string) => {
    const parts = value.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0, 1];
    return { rgb: parts.slice(0, 3), a: parts.length > 3 ? parts[3] : 1 };
  };
  const over = (fg: { rgb: number[]; a: number }, bg: number[]) =>
    fg.rgb.map((channel, index) => channel * fg.a + bg[index] * (1 - fg.a));

  const solidBehind = (el: Element) => {
    const layers: { rgb: number[]; a: number }[] = [];
    let node: Element | null = el;
    while (node) {
      const colour = parse(getComputedStyle(node).backgroundColor);
      if (colour.a > 0) layers.push(colour);
      if (colour.a === 1) break;
      node = node.parentElement;
    }
    let base =
      layers.length > 0 && layers[layers.length - 1].a === 1
        ? (layers.pop() as { rgb: number[] }).rgb
        : [0, 0, 0];
    for (const layer of layers.reverse()) base = over(layer, base);
    return base;
  };

  const luminance = (rgb: number[]) => {
    const [r, g, b] = rgb.map((channel) => {
      const s = channel / 255;
      return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };

  (window as unknown as { __contrast: (el: Element) => number }).__contrast = (el) => {
    const behind = solidBehind(el);
    const ink = over(parse(getComputedStyle(el).color), behind);
    const [lighter, darker] = [luminance(ink), luminance(behind)].sort((a, b) => b - a);
    return (lighter + 0.05) / (darker + 0.05);
  };
};

function contrastOf(page: import("@playwright/test").Page, selector: string) {
  return page.evaluate((css) => {
    const el = document.querySelector(css);
    if (!el) throw new Error(`nothing matched ${css}`);
    return (window as unknown as { __contrast: (el: Element) => number }).__contrast(el);
  }, selector);
}

test.describe("dark theme: brand-coloured text", () => {
  test.use({ colorScheme: "dark" });

  test.beforeEach(async ({ page }) => {
    // The theme is the reader's choice, kept in storage — set before the app boots so it
    // renders dark from the first paint rather than being toggled mid-test.
    await page.addInitScript(() => window.localStorage.setItem("kagent.themeMode", "dark"));
    await page.addInitScript(PROBE);
  });

  test("a prompt library's name is readable", async ({ page }) => {
    await page.goto("/prompts");
    await expect(page.locator(".ant-table-tbody a").first()).toBeVisible();

    // Was 2.54:1 — the only navigable text on the row and the hardest thing to read.
    expect(await contrastOf(page, ".ant-table-tbody a")).toBeGreaterThanOrEqual(
      AA_SMALL_TEXT,
    );
  });

  test("a tool server's kind is readable", async ({ page }) => {
    await page.goto("/mcp");
    await expect(page.locator(".ant-table-tbody .ant-tag").first()).toBeVisible();

    // antd's presets are derived for a light page: these were 3.4:1 and 4.2:1.
    const tags = await page.evaluate(() =>
      [...document.querySelectorAll(".ant-table-tbody .ant-tag")].map((tag) =>
        (window as unknown as { __contrast: (el: Element) => number }).__contrast(tag),
      ),
    );
    expect(tags.length).toBeGreaterThan(0);
    for (const ratio of tags) expect(ratio).toBeGreaterThanOrEqual(AA_SMALL_TEXT);
  });

  test("the tab you are on is the one you can read", async ({ page }) => {
    /*
     * The same mistake as the toggle below, in a third place.
     *
     * antd colours a selected tab and its ink bar from `colorPrimary`, which is the
     * deep purple chosen as a *fill* with light text on it. Used as ink on the dark
     * theme's near-black page the selected tab measured 2.37:1 while the unselected
     * ones sat at 19:1 — so the tab you were not on was the one you could read, which
     * is exactly backwards.
     *
     * Every tab is measured rather than only the selected one: a fix that made the
     * active tab legible by dimming the rest would pass a check aimed at one of them.
     */
    await page.goto("/agents");
    await expect(page.locator('[role="tab"]').first()).toBeVisible();

    const ratios = await page.evaluate(() =>
      [...document.querySelectorAll('[role="tab"]')].map((tab) =>
        (window as unknown as { __contrast: (el: Element) => number }).__contrast(tab),
      ),
    );
    expect(ratios.length).toBeGreaterThan(1);
    for (const ratio of ratios) expect(ratio).toBeGreaterThanOrEqual(AA_SMALL_TEXT);
  });

  test("the selected half of a toggle is readable", async ({ page }) => {
    // The model form's authentication toggle. It used to be the agent form's type
    // toggle, and that form is gone — creating an agent is now choosing a harness and
    // a template, which are pickers rather than radio buttons. The property is the
    // theme's, not the page's, so any checked radio button measures it.
    await page.goto("/models/new");
    await expect(page.getByTestId("model-auth-type")).toBeVisible();

    // Was 2.2:1 against 13:1 for the unselected half — the option you had *not* chosen was
    // the one you could read.
    expect(
      await contrastOf(page, ".ant-radio-button-wrapper-checked"),
    ).toBeGreaterThanOrEqual(AA_SMALL_TEXT);
  });
});

/**
 * A control's own edges, which a contrast check aimed at text walks straight past.
 *
 * The segmented control was reported as hard to read on the dark theme while measuring
 * fine for text — 12.6:1 selected, 8.2:1 not. What was missing was the control: its
 * track measured 1.00:1 against the page, the same colour, and its selected pill 1.12:1
 * against the track, behind an elevation shadow antd paints at one percent white. Both
 * halves were legible and nothing said which one was chosen.
 *
 * Non-text contrast is a separate threshold and needs measuring separately, which is
 * what this block does: 3:1 for the edge that identifies the state, with the text
 * checks kept beside it so that edge cannot be won by dimming the labels.
 */
test.describe("dark theme: control edges", () => {
  test.use({ colorScheme: "dark" });

  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => window.localStorage.setItem("kagent.themeMode", "dark"));
    await page.addInitScript(PROBE);
  });

  test("which half of a segmented control is chosen is visible, not just legible", async ({
    page,
  }) => {
    await page.goto("/mcp/new");
    await expect(page.getByTestId("mcp-kind")).toBeVisible();

    const surfaces = await page.evaluate(() => {
      const track = document.querySelector('[data-testid="mcp-kind"]');
      const pill = document.querySelector(
        '[data-testid="mcp-kind"] .ant-segmented-item-selected',
      );
      if (!track || !pill) throw new Error("the segmented control is not on the page");
      return {
        ring: getComputedStyle(pill).boxShadow,
        track: getComputedStyle(track).backgroundColor,
        page: getComputedStyle(document.body).backgroundColor,
      };
    });

    /*
     * The pill's edge, against the trough it sits in and against the page around it. A
     * fill cannot carry this at the dark end — the page is near-black, so 3:1 on
     * luminance alone takes a mid-grey pill that reads as disabled — so the theme
     * outlines the pill instead, the way it outlines every other control.
     */
    const track = colourOf(surfaces.track).rgb;
    const behind = colourOf(surfaces.page).rgb;
    // Flattened onto what each sits on before measuring, so a shadow that is barely
    // there measures as barely there.
    const ringOnTrack = over(colourOf(surfaces.ring), track);
    const ringOnPage = over(colourOf(surfaces.ring), behind);

    expect(contrast(ringOnTrack, track)).toBeGreaterThanOrEqual(AA_NON_TEXT);
    expect(contrast(ringOnPage, behind)).toBeGreaterThanOrEqual(AA_NON_TEXT);
  });

  test("and both of its labels stay readable", async ({ page }) => {
    await page.goto("/mcp/new");
    await expect(page.getByTestId("mcp-kind")).toBeVisible();

    const ratios = await page.evaluate(() =>
      [
        ...document.querySelectorAll(
          '[data-testid="mcp-kind"] .ant-segmented-item-label',
        ),
      ].map((label) =>
        (window as unknown as { __contrast: (el: Element) => number }).__contrast(label),
      ),
    );
    expect(ratios.length).toBe(2);
    for (const ratio of ratios) expect(ratio).toBeGreaterThanOrEqual(AA_SMALL_TEXT);
  });
});
