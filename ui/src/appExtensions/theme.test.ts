import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { resolveAntdTheme, resolveAppTheme, resolveSupportedModes } from "./theme";

/**
 * How the installed extensions' tokens combine with the application's two palettes.
 *
 * This is the contract a product with its own design language depends on, and the
 * precedence is the whole of it: the app supplies a palette per mode, an extension
 * may override anything mode-independent once, and may override per mode where a
 * value only makes sense against one background. With more than one installed, the
 * later entry wins — the last block below is that rule.
 */

describe("resolveAppTheme", () => {
  it("returns the app's own palette for the mode when nothing is installed", () => {
    expect(resolveAppTheme([], "light").color.bg).toBe(
      themeFor("light").color.bg,
    );
    expect(resolveAppTheme([], "dark").color.bg).toBe(themeFor("dark").color.bg);
  });

  it("applies mode-independent tokens in both modes", () => {
    // A brand's primary does not change because the reader prefers light, so an
    // extension should not have to state it twice.
    const extension = { tokens: { color: { primary: "#ff0000" } } };

    expect(resolveAppTheme([extension], "dark").color.primary).toBe("#ff0000");
    expect(resolveAppTheme([extension], "light").color.primary).toBe("#ff0000");
  });

  it("applies per-mode tokens only in their own mode", () => {
    const extension = {
      modeTokens: {
        dark: { color: { border: "#111111" } },
        light: { color: { border: "#eeeeee" } },
      },
    };

    expect(resolveAppTheme([extension], "dark").color.border).toBe("#111111");
    expect(resolveAppTheme([extension], "light").color.border).toBe("#eeeeee");
  });

  it("lets a per-mode token win over a mode-independent one", () => {
    // The point of having both: state the general case once, then correct it where a
    // background demands something different.
    const extension = {
      tokens: { color: { border: "#888888" } },
      modeTokens: { light: { color: { border: "#dddddd" } } },
    };

    expect(resolveAppTheme([extension], "light").color.border).toBe("#dddddd");
    // Dark has no override of its own, so it keeps the shared value rather than
    // falling back to the app's.
    expect(resolveAppTheme([extension], "dark").color.border).toBe("#888888");
  });

  it("leaves untouched groups alone", () => {
    const extension = { modeTokens: { dark: { color: { primary: "#ff0000" } } } };
    const resolved = resolveAppTheme([extension], "dark");

    expect(resolved.radius).toEqual(themeFor("dark").radius);
    expect(resolved.font).toEqual(themeFor("dark").font);
    // The spacing scale is a function every component calls and is deliberately not
    // overridable; it must survive the merge intact.
    expect(resolved.space(4)).toBe(themeFor("dark").space(4));
  });
});

describe("resolveAntdTheme", () => {
  it("hands the component library the resolved colours for the mode", () => {
    const extension = { modeTokens: { light: { color: { primary: "#00ff00" } } } };

    expect(resolveAntdTheme([extension], "light").token?.colorPrimary).toBe("#00ff00");
    expect(resolveAntdTheme([extension], "dark").token?.colorPrimary).toBe(
      themeFor("dark").color.primary,
    );
  });

  it("surfaces follow the mode, so panels are not left dark on a light page", () => {
    expect(resolveAntdTheme([], "light").token?.colorBgContainer).toBe(
      themeFor("light").color.bgElevated,
    );
  });

  it("lets an extension's own antd block contradict what its tokens imply", () => {
    // A product may want a different radius on buttons from its cards, which no
    // single token can express — so the explicit block is applied last.
    const extension = {
      tokens: { radius: { md: 2 } },
      antd: { token: { borderRadius: 20 } },
    };

    expect(resolveAntdTheme([extension], "dark").token?.borderRadius).toBe(20);
  });

  /*
   * A component's tokens are an object, so spreading the two `components` maps together
   * let an extension naming one property of `Input` discard every property the
   * application had set on it — including the border and placeholder colours picked for
   * contrast. The search box on a page the extension never touched came out at 1.26:1.
   */
  it("keeps the application's other tokens for a component an extension touches", () => {
    const extension = { antd: { components: { Input: { controlHeight: 36 } } } };

    const input = resolveAntdTheme([extension], "dark").components?.Input;

    expect(input?.controlHeight).toBe(36);
    expect(input?.colorBorder).toBe(themeFor("dark").color.borderStrong);
  });

  it("still lets an extension contradict a token the application set on that component", () => {
    const extension = { antd: { components: { Input: { colorBorder: "#abcdef" } } } };

    expect(resolveAntdTheme([extension], "dark").components?.Input?.colorBorder).toBe(
      "#abcdef",
    );
  });
});

describe("several installed themes", () => {
  const first = {
    tokens: { color: { primary: "#111111", success: "#00aa00" } },
  };
  const second = { tokens: { color: { primary: "#222222" } } };

  it("lets the later extension win the token both name", () => {
    expect(resolveAppTheme([first, second], "dark").color.primary).toBe("#222222");
  });

  it("keeps a token only the earlier extension names", () => {
    // The whole point of merging rather than picking one theme: installing a second
    // extension that restyles the primary must not discard the first one's palette.
    expect(resolveAppTheme([first, second], "dark").color.success).toBe("#00aa00");
  });

  it("folds antd component tokens from both, later winning per token", () => {
    const merged = resolveAntdTheme(
      [
        { antd: { components: { Input: { controlHeight: 36 } } } },
        { antd: { components: { Input: { colorBorder: "#abcdef" } } } },
      ],
      "dark",
    );

    expect(merged.components?.Input?.controlHeight).toBe(36);
    expect(merged.components?.Input?.colorBorder).toBe("#abcdef");
  });
});

describe("resolveSupportedModes", () => {
  it("leaves the choice open when no installed extension restricts it", () => {
    expect(resolveSupportedModes([])).toBeUndefined();
    expect(resolveSupportedModes([{ tokens: {} }])).toBeUndefined();
  });

  it("honours the one extension that restricts the palette", () => {
    expect(resolveSupportedModes([{ supportedModes: ["dark"] }])).toEqual(["dark"]);
  });

  /*
   * An intersection rather than last-one-wins, and this is the case that decides it:
   * `supportedModes` says an extension's own components cannot be read in the other
   * palette. Installing a second extension does not make the first one's components
   * legible, so a mode neither can honour together must not be offered.
   */
  it("offers only the modes every restricting extension can honour", () => {
    expect(
      resolveSupportedModes([
        { supportedModes: ["dark", "light"] },
        { supportedModes: ["dark"] },
      ]),
    ).toEqual(["dark"]);
  });

  it("ignores extensions that state no restriction", () => {
    expect(
      resolveSupportedModes([{ tokens: {} }, { supportedModes: ["light"] }]),
    ).toEqual(["light"]);
  });
});
