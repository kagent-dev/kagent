import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { resolveAntdTheme, resolveAppTheme } from "./theme";

/**
 * How an extension's tokens combine with the application's two palettes.
 *
 * This is the contract a product with its own design language depends on, and the
 * precedence is the whole of it: the app supplies a palette per mode, the extension
 * may override anything mode-independent once, and may override per mode where a
 * value only makes sense against one background.
 */

describe("resolveAppTheme", () => {
  it("returns the app's own palette for the mode when an extension supplies none", () => {
    expect(resolveAppTheme(undefined, "light").color.bg).toBe(
      themeFor("light").color.bg,
    );
    expect(resolveAppTheme(undefined, "dark").color.bg).toBe(themeFor("dark").color.bg);
  });

  it("applies mode-independent tokens in both modes", () => {
    // A brand's primary does not change because the reader prefers light, so an
    // extension should not have to state it twice.
    const vendor = { tokens: { color: { primary: "#ff0000" } } };

    expect(resolveAppTheme(vendor, "dark").color.primary).toBe("#ff0000");
    expect(resolveAppTheme(vendor, "light").color.primary).toBe("#ff0000");
  });

  it("applies per-mode tokens only in their own mode", () => {
    const vendor = {
      modeTokens: {
        dark: { color: { border: "#111111" } },
        light: { color: { border: "#eeeeee" } },
      },
    };

    expect(resolveAppTheme(vendor, "dark").color.border).toBe("#111111");
    expect(resolveAppTheme(vendor, "light").color.border).toBe("#eeeeee");
  });

  it("lets a per-mode token win over a mode-independent one", () => {
    // The point of having both: state the general case once, then correct it where a
    // background demands something different.
    const vendor = {
      tokens: { color: { border: "#888888" } },
      modeTokens: { light: { color: { border: "#dddddd" } } },
    };

    expect(resolveAppTheme(vendor, "light").color.border).toBe("#dddddd");
    // Dark has no override of its own, so it keeps the shared value rather than
    // falling back to the app's.
    expect(resolveAppTheme(vendor, "dark").color.border).toBe("#888888");
  });

  it("leaves untouched groups alone", () => {
    const vendor = { modeTokens: { dark: { color: { primary: "#ff0000" } } } };
    const resolved = resolveAppTheme(vendor, "dark");

    expect(resolved.radius).toEqual(themeFor("dark").radius);
    expect(resolved.font).toEqual(themeFor("dark").font);
    // The spacing scale is a function every component calls and is deliberately not
    // overridable; it must survive the merge intact.
    expect(resolved.space(4)).toBe(themeFor("dark").space(4));
  });
});

describe("resolveAntdTheme", () => {
  it("hands the component library the resolved colours for the mode", () => {
    const vendor = { modeTokens: { light: { color: { primary: "#00ff00" } } } };

    expect(resolveAntdTheme(vendor, "light").token?.colorPrimary).toBe("#00ff00");
    expect(resolveAntdTheme(vendor, "dark").token?.colorPrimary).toBe(
      themeFor("dark").color.primary,
    );
  });

  it("surfaces follow the mode, so panels are not left dark on a light page", () => {
    expect(resolveAntdTheme(undefined, "light").token?.colorBgContainer).toBe(
      themeFor("light").color.bgElevated,
    );
  });

  it("lets an extension's own antd block contradict what its tokens imply", () => {
    // A product may want a different radius on buttons from its cards, which no
    // single token can express — so the explicit block is applied last.
    const vendor = {
      tokens: { radius: { md: 2 } },
      antd: { token: { borderRadius: 20 } },
    };

    expect(resolveAntdTheme(vendor, "dark").token?.borderRadius).toBe(20);
  });

  /*
   * A component's tokens are an object, so spreading the two `components` maps together
   * let an extension naming one property of `Input` discard every property the
   * application had set on it — including the border and placeholder colours picked for
   * contrast. The search box on a page the extension never touched came out at 1.26:1.
   */
  it("keeps the application's other tokens for a component an extension touches", () => {
    const vendor = { antd: { components: { Input: { controlHeight: 36 } } } };

    const input = resolveAntdTheme(vendor, "dark").components?.Input;

    expect(input?.controlHeight).toBe(36);
    expect(input?.colorBorder).toBe(themeFor("dark").color.borderStrong);
  });

  it("still lets an extension contradict a token the application set on that component", () => {
    const vendor = { antd: { components: { Input: { colorBorder: "#abcdef" } } } };

    expect(resolveAntdTheme(vendor, "dark").components?.Input?.colorBorder).toBe(
      "#abcdef",
    );
  });
});
