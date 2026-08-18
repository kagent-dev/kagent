import React from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeToggle } from "@/components/ThemeToggle";

const setTheme = jest.fn();
let currentTheme: string | undefined = "system";

jest.mock("next-themes", () => ({
  useTheme: () => ({ theme: currentTheme, setTheme }),
}));

beforeEach(() => {
  setTheme.mockClear();
  currentTheme = "system";
});

describe("ThemeToggle", () => {
  it("advances system -> light -> dark -> system on click", async () => {
    const user = userEvent.setup();

    const { rerender } = render(<ThemeToggle />);
    await user.click(screen.getByRole("button", { name: "Theme: system" }));
    expect(setTheme).toHaveBeenLastCalledWith("light");

    currentTheme = "light";
    rerender(<ThemeToggle />);
    await user.click(screen.getByRole("button", { name: "Theme: light" }));
    expect(setTheme).toHaveBeenLastCalledWith("dark");

    // The control previously offered only light and dark, so "system" became
    // unreachable once either was picked. Getting back to it is the point.
    currentTheme = "dark";
    rerender(<ThemeToggle />);
    await user.click(screen.getByRole("button", { name: "Theme: dark" }));
    expect(setTheme).toHaveBeenLastCalledWith("system");
  });

  it("applies the change on a single click, without opening a menu", async () => {
    const user = userEvent.setup();
    render(<ThemeToggle />);

    const button = screen.getByRole("button", { name: "Theme: system" });
    expect(button).not.toHaveAttribute("aria-haspopup");

    await user.click(button);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(setTheme).toHaveBeenCalledTimes(1);
  });

  it("falls back to system when the persisted value is unrecognised", () => {
    currentTheme = "solarized";
    render(<ThemeToggle />);
    expect(screen.getByRole("button", { name: "Theme: system" })).toBeInTheDocument();
  });
});
