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

function renderCard(onDismiss = vi.fn()) {
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <RecapCard recap={RECAP} onDismiss={onDismiss} />
    </ThemeProvider>,
  );
}

describe("RecapCard", () => {
  it("carries the recap text", () => {
    renderCard();
    expect(within(screen.getByTestId("chat-recap")).getByText(RECAP.text)).toBeInTheDocument();
  });

  it("says when it was made on its top line, in a machine-readable time", () => {
    renderCard();
    const card = screen.getByTestId("chat-recap");
    expect(within(card).getByText(/^Recap from/)).toBeInTheDocument();
    expect(card.querySelector("time")).toHaveAttribute("datetime", RECAP.generatedAt);
  });

  it("is not an agent reply", () => {
    renderCard();
    expect(screen.queryByTestId("chat-message")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-recap")).not.toHaveAttribute("data-role");
  });

  it("has no separate dismiss button", () => {
    renderCard();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Dismiss recap" })).not.toBeInTheDocument();
  });

  it("collapses when clicked anywhere on it", () => {
    const onDismiss = vi.fn();
    renderCard(onDismiss);
    fireEvent.click(screen.getByText(RECAP.text));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
