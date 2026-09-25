import type { SerializedStyles } from "@emotion/react";
import type { ThemeConfig } from "antd";
import { antdThemeFor, themeFor } from "@/theme/theme";
import type { AppTheme, ThemeMode } from "@/theme/theme";

/**
 * The parts of the design tokens an extension may override.
 *
 * Deliberately not a deep partial of the whole token object: the spacing scale
 * is a function every component calls, and letting it be replaced would make
 * layout unpredictable in ways no reviewer could anticipate. Colour, radius,
 * font and layout metrics are the surface a different-looking product actually
 * needs.
 */
export interface ExtensionThemeTokens {
  color?: Partial<AppTheme["color"]>;
  radius?: Partial<AppTheme["radius"]>;
  font?: Partial<AppTheme["font"]>;
  layout?: Partial<AppTheme["layout"]>;
}

/**
 * How an extension changes the way the application looks.
 *
 * Restyling has to reach further than a slot does. A component mounted at a
 * point can only change what is inside it, whereas a product with its own
 * design language needs the application's own components to look different
 * too — its buttons, tables, inputs and headings, none of which it owns.
 *
 * Overriding tokens is what achieves that: the application's components read
 * every colour, radius and font from them, so replacing the values restyles
 * components the extension never touches. `antd` covers the component library's
 * own internals, `globalStyles` anything neither reaches, and `stylesheets`
 * exists because a web font has to be fetched before the first paint.
 */
export interface ExtensionTheme {
  /**
   * Overrides the application's design tokens, and so its own components.
   *
   * Applied in both modes. For a value that only makes sense in one — a surface,
   * a border, anything that has to contrast with the page — use `modeTokens`.
   */
  tokens?: ExtensionThemeTokens;
  /**
   * Per-mode overrides, applied after `tokens` and winning over them.
   *
   * A product with its own design language needs two palettes, not one: a border
   * that reads correctly on a near-black page is invisible on a white one, so an
   * extension that could only state one set of colours would either break in light
   * mode or have to force the whole application dark. Anything mode-independent —
   * the brand's own primary, a font, a radius — still belongs in `tokens`, so it
   * does not have to be repeated.
   */
  modeTokens?: Partial<Record<ThemeMode, ExtensionThemeTokens>>;
  /** Merged into antd's ConfigProvider, for the component library's internals. */
  antd?: ThemeConfig;
  /**
   * Appended after the application's own global styles, so it wins on ties.
   * For what tokens cannot reach — gradient borders, scrollbars, resets.
   */
  globalStyles?: SerializedStyles;
  /** Stylesheet URLs loaded before the first render, e.g. a web font. */
  stylesheets?: readonly string[];
  /**
   * Which palettes this extension can actually be read in. Both, by default.
   *
   * A product whose component library ships one theme cannot honour the other: its
   * own components paint their text a fixed near-white, so a light page renders
   * white-on-white and no amount of token overriding reaches inside them. Declaring
   * `["dark"]` is how such a product says so — the mode is pinned and the toggle is
   * not offered, which is better than a control that makes the app unreadable.
   *
   * This is a statement about the extension's components, not a preference. An
   * extension that can do both should say nothing here.
   */
  supportedModes?: readonly ThemeMode[];
}

/**
 * The app's tokens with every installed extension's overrides folded in, one level
 * deep, in install order — so the later of two extensions naming the same colour
 * is the one that gets it.
 *
 * The mode picks which palette the overrides are folded *into*. An extension that
 * names a colour still wins in both modes, which is intended: a product with its
 * own brand does not become a different brand because the reader prefers light.
 */
export function resolveAppTheme(
  themes: readonly ExtensionTheme[] = [],
  mode: ThemeMode = "dark",
): AppTheme {
  // Three layers per extension, each winning over the last: what is resolved so
  // far, the extension's mode-independent tokens, then its tokens for this mode.
  return themes.reduce<AppTheme>((resolved, theme) => {
    const shared = theme.tokens;
    const forMode = theme.modeTokens?.[mode];
    if (!shared && !forMode) return resolved;

    return {
      ...resolved,
      color: { ...resolved.color, ...shared?.color, ...forMode?.color },
      radius: { ...resolved.radius, ...shared?.radius, ...forMode?.radius },
      font: { ...resolved.font, ...shared?.font, ...forMode?.font },
      layout: { ...resolved.layout, ...shared?.layout, ...forMode?.layout },
    };
  }, themeFor(mode));
}

/**
 * The antd config the installed themes produce.
 *
 * Each extension's own antd block is applied after the tokens it implies, so it can
 * contradict them — a product may want a different button radius from its cards,
 * which a single token cannot express. Blocks are folded in install order, so the
 * later extension wins where two name the same antd token.
 */
export function resolveAntdTheme(
  themes: readonly ExtensionTheme[] = [],
  mode: ThemeMode = "dark",
): ThemeConfig {
  const resolved = resolveAppTheme(themes, mode);
  const antdTheme = antdThemeFor(mode);

  const base: ThemeConfig = {
    ...antdTheme,
    token: {
      ...antdTheme.token,
      colorPrimary: resolved.color.primary,
      colorBgBase: resolved.color.bg,
      colorTextBase: resolved.color.text,
      colorBorder: resolved.color.border,
      colorBgContainer: resolved.color.bgElevated,
      colorBgElevated: resolved.color.bgElevated,
      colorSuccess: resolved.color.success,
      colorWarning: resolved.color.warning,
      colorError: resolved.color.danger,
      borderRadius: resolved.radius.md,
      fontFamily: resolved.font.body,
    },
  };

  return themes.reduce<ThemeConfig>(
    (config, theme) => ({
      ...config,
      ...theme.antd,
      token: { ...config.token, ...theme.antd?.token },
      components: mergedComponents(config.components, theme.antd?.components),
    }),
    base,
  );
}

/**
 * Component tokens merged per component, not per block.
 *
 * Spreading the two `components` objects together looks equivalent and is not: the value
 * under `Input` is itself an object, so an extension naming one property of it replaced
 * every property the application had set. A skin asking for `Input: { controlHeight: 36 }`
 * silently gave up the border and placeholder colours tuned for contrast, and the input
 * fell back to the plain border token — which is a divider, and measured 1.26:1 around a
 * search box on a page the extension had not touched at all.
 *
 * One level deeper is the whole fix, and it is the semantics the extension point already
 * claims: an extension overrides what it names. Two levels would be wrong — a component's
 * token values are colours and numbers, not objects to be merged.
 */
function mergedComponents(
  base: ThemeConfig["components"],
  overrides: ThemeConfig["components"],
): ThemeConfig["components"] {
  if (!overrides) return base;
  if (!base) return overrides;

  const merged: Record<string, unknown> = { ...base };
  for (const [component, tokens] of Object.entries(overrides)) {
    const existing = merged[component];
    merged[component] =
      existing && typeof existing === "object" && typeof tokens === "object"
        ? { ...existing, ...tokens }
        : tokens;
  }

  return merged as ThemeConfig["components"];
}

/**
 * Adds every installed extension's stylesheets to the document head.
 *
 * Called before the first render rather than from a component: a font arriving
 * afterwards reflows everything that has already painted. Already-present hrefs are
 * skipped, so two extensions asking for the same web font fetch it once.
 */
export function loadExtensionStylesheets(
  themes: readonly ExtensionTheme[] = [],
): void {
  for (const href of themes.flatMap((theme) => theme.stylesheets ?? [])) {
    if (document.querySelector(`link[rel="stylesheet"][href="${href}"]`)) continue;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = href;
    document.head.append(link);
  }
}

/**
 * The palettes the whole install can be read in, or `undefined` when no extension
 * restricts them.
 *
 * An intersection, not a last-one-wins: `supportedModes` is a statement that an
 * extension's own components cannot be read in the other palette, and installing a
 * second extension does not make the first one's components legible. So a mode is
 * offered only while every extension that has an opinion can honour it.
 */
export function resolveSupportedModes(
  themes: readonly ExtensionTheme[] = [],
): readonly ThemeMode[] | undefined {
  const declared = themes
    .map((theme) => theme.supportedModes)
    .filter((modes): modes is readonly ThemeMode[] => modes !== undefined);

  if (declared.length === 0) return undefined;

  return declared.reduce((supported, modes) =>
    supported.filter((mode) => modes.includes(mode)),
  );
}
