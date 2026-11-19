import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ThemeModeContext, type ThemeModeContextValue } from "@/theme/useThemeMode";
import { ProviderIconWithLabel } from "./ProviderIcon";

const inMode = (mode: "light" | "dark") =>
  ({ mode, isExplicit: true, canToggle: true, setMode: () => {}, toggle: () => {} }) satisfies ThemeModeContextValue;

describe("ProviderIconWithLabel", () => {
  it("shows a tinted icon beside the provider's name, in the colour for the mode", () => {
    const { rerender } = render(
      <ThemeModeContext.Provider value={inMode("light")}>
        <ProviderIconWithLabel type="OpenAI" label="OpenAI" />
      </ThemeModeContext.Provider>,
    );
    expect(screen.getByText("OpenAI")).toBeVisible();
    expect(screen.getByTestId("provider-badge").querySelector("svg")).not.toBeNull();
    const light = getComputedStyle(screen.getByTestId("provider-badge")).color;

    rerender(
      <ThemeModeContext.Provider value={inMode("dark")}>
        <ProviderIconWithLabel type="OpenAI" label="OpenAI" />
      </ThemeModeContext.Provider>,
    );
    expect(getComputedStyle(screen.getByTestId("provider-badge")).color).not.toBe(light);
  });

  it("falls back to the provider's initial when it has no icon", () => {
    render(<ProviderIconWithLabel type="SAPAICore" label="SAP AI Core" />);
    expect(screen.getByTestId("provider-badge")).toHaveTextContent(/^S$/);
    expect(screen.getByText("SAP AI Core")).toBeVisible();
  });
});
