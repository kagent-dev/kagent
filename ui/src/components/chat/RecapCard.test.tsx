import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { ConversationRecap } from "@/api";
import { RecapCard } from "./RecapCard";

const RECAP: ConversationRecap = {
  text: "Checkout was crashlooping on a failing liveness probe.",
  turnCount: 3,
  generatedAt: "2026-08-21T10:30:00Z",
};
const STARTED_AT = "2026-08-21T10:00:00Z";

function renderCard(options: { collapsed?: boolean; onToggle?: () => void; startedAt?: string } = {}) {
  const { collapsed = false, onToggle = vi.fn() } = options;
  const startedAt = "startedAt" in options ? options.startedAt : STARTED_AT;
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <RecapCard recap={RECAP} startedAt={startedAt} collapsed={collapsed} onToggle={onToggle} />
    </ThemeProvider>,
  );
}

describe("RecapCard", () => {
  it("pads like a chat reply", () => {
    renderCard();
    expect(getComputedStyle(screen.getByTestId("chat-recap")).padding).toBe("8px 12px");
  });

  it("carries the recap text", () => {
    renderCard();
    expect(within(screen.getByTestId("chat-recap")).getByText(RECAP.text)).toBeInTheDocument();
  });

  it("says how much of the conversation it covers on its top line, in a machine-readable time", () => {
    renderCard();
    const card = screen.getByTestId("chat-recap");
    expect(within(card).getByText("Recap of the last 30 minutes")).toBeInTheDocument();
    expect(card.querySelector("time")).toHaveAttribute("datetime", RECAP.generatedAt);
  });

  it("says only 'Recap' when it does not know when the conversation began", () => {
    renderCard({ startedAt: undefined });
    expect(screen.getByText("Recap")).toBeInTheDocument();
  });

  it("is not an agent reply", () => {
    renderCard();
    expect(screen.queryByTestId("chat-message")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-recap")).not.toHaveAttribute("data-role");
  });

  it("is one button with no separate dismiss control", () => {
    renderCard();
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("shows its text and reports itself expanded", () => {
    renderCard();
    expect(screen.getByTestId("chat-recap")).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText(RECAP.text)).toBeInTheDocument();
  });

  it("keeps only its header when collapsed, and reports itself collapsed", () => {
    renderCard({ collapsed: true });
    expect(screen.getByTestId("chat-recap")).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText(RECAP.text)).not.toBeInTheDocument();
    expect(screen.getByText("Recap of the last 30 minutes")).toBeInTheDocument();
  });

  it("toggles when clicked anywhere on it", () => {
    const onToggle = vi.fn();
    renderCard({ onToggle });
    fireEvent.click(screen.getByText(RECAP.text));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});

/** WCAG relative luminance of an sRGB colour. */
function luminance([r, g, b]: number[]): number {
  const [lr, lg, lb] = [r, g, b].map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * lr + 0.7152 * lg + 0.0722 * lb;
}
const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
const blend = (top: number[], bottom: number[], share: number) => top.map((v, i) => v * share + bottom[i] * (1 - share));
const contrast = (a: number[], b: number[]) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

describe("RecapCard contrast", () => {
  it.each(["light", "dark"] as const)("keeps the header and text above 4.5:1 on the %s theme, even pressed", (mode) => {
    const { color } = themeFor(mode);
    const text = rgb(color.text);
    const pressed = blend(text, rgb(color.bg), 0.05);
    expect(contrast(rgb(color.textMuted), pressed)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(blend(text, pressed, 0.7), pressed)).toBeGreaterThanOrEqual(4.5);
  });
});
