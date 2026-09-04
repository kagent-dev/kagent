import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ChatMessageItem } from "./ChatMessageItem";

describe("ChatMessageItem interaction layout", () => {
  it("gives a short user-carried ask_user record the full notification lane", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatMessageItem
          message={{
            id: "answer-1",
            role: "user",
            createdAt: "2026-09-03T00:00:00Z",
            parts: [
              {
                kind: "ask_user",
                interaction: {
                  questions: [
                    { question: "Continue?", choices: ["Yes", "No"], multiple: false },
                  ],
                  answers: [["No"]],
                },
              },
            ],
          }}
        />
      </ThemeProvider>,
    );

    expect(getComputedStyle(screen.getByTestId("chat-message-content")).width).toBe("100%");
    expect(getComputedStyle(screen.getByTestId("chat-message")).justifyItems).toBe("start");
  });

  it("keeps a tool rejection in the right-aligned user-response lane", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatMessageItem
          message={{
            id: "decision-1",
            role: "user",
            createdAt: "2026-09-03T00:00:00Z",
            parts: [
              {
                kind: "tool_approval",
                approval: {
                  tools: [{ id: "approval-1", name: "delete_pod", args: {} }],
                  decisions: [{ id: "approval-1", approved: false }],
                },
              },
            ],
          }}
        />
      </ThemeProvider>,
    );

    expect(getComputedStyle(screen.getByTestId("chat-message-content")).width).toBe("auto");
    expect(getComputedStyle(screen.getByTestId("chat-message")).justifyItems).toBe("end");
  });
});
