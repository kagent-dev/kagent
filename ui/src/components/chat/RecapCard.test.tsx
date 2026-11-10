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
  it("is a labelled region carrying the recap text", () => {
    renderCard();
    const card = screen.getByRole("region", { name: "Recap" });
    expect(within(card).getByText(RECAP.text)).toBeInTheDocument();
  });

  it("says when it was made, in a machine-readable time", () => {
    renderCard();
    const card = screen.getByRole("region", { name: "Recap" });
    expect(within(card).getByText(/^Written /)).toBeInTheDocument();
    expect(card.querySelector("time")).toHaveAttribute("datetime", RECAP.generatedAt);
  });

  it("is not an agent reply", () => {
    renderCard();
    expect(screen.queryByTestId("chat-message")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-recap")).not.toHaveAttribute("data-role");
  });

  it("calls onDismiss when the dismiss button is clicked", () => {
    const onDismiss = vi.fn();
    renderCard(onDismiss);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss recap" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
