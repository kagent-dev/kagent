import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { AskUserRecord } from "./AskUserRecord";

describe("AskUserRecord", () => {
  it("renders a completed exchange without raw protocol content", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <AskUserRecord
          part={{
            kind: "ask_user",
            interaction: {
              questions: [
                { question: "What size?", choices: ["Small", "Large"], multiple: false },
                { question: "Which toppings?", choices: [], multiple: true },
              ],
              answers: [["Large"], ["Mushroom", "Pineapple"]],
              askedBy: "planner",
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText("Question answered")).toBeTruthy();
    expect(screen.getByText("What size?")).toBeTruthy();
    expect(screen.getByText("Large")).toBeTruthy();
    expect(screen.getByText("Mushroom, Pineapple")).toBeTruthy();
    expect(screen.getByText("for planner")).toBeTruthy();
    expect(screen.queryByText("ask_user")).toBeNull();
  });
});
