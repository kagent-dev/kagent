import { ThemeProvider } from "@emotion/react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstanceRecovery, ChatController } from "@/api";
import { ChatTranscript } from "./ChatTranscript";

const NOTE = /does not remember this message/;

const chat: ChatController = {
  messages: [
    {
      id: "captured-answer", taskId: "task", role: "user", createdAt: "2026-10-22T00:00:00Z",
      parts: [{ kind: "ask_user", interaction: {
        questions: [{ question: "Captured question?", choices: ["Retained"], multiple: false }],
        answers: [["Retained"]],
      } }],
    },
    { id: "discarded", taskId: "task", role: "user", createdAt: "2026-10-22T00:00:01Z", parts: [{ kind: "text", text: "Lost reply" }] },
    { id: "partial", taskId: "task", role: "agent", createdAt: "2026-10-22T00:00:02Z", parts: [{ kind: "text", text: "Half a story" }] },
    { id: "next-turn", taskId: "next", role: "agent", createdAt: "2026-10-22T00:00:03Z", parts: [{ kind: "text", text: "Later turn" }] },
  ],
  isLoadingHistory: false,
  turnState: "idle", turnPhase: "idle", phase: "idle",
  refreshTranscript: vi.fn(), send: vi.fn(), cancel: vi.fn(), dismissQuestion: vi.fn(),
  answerQuestion: vi.fn(), answerToolApproval: vi.fn(), retry: vi.fn(),
};

function transcript(recovery: AgentInstanceRecovery) {
  return <ThemeProvider theme={themeFor("dark")}><ChatTranscript chat={chat} recovery={recovery} /></ThemeProvider>;
}

function message(id: string) {
  return screen.getAllByTestId("chat-message").find((node) => node.dataset.messageId === id)!;
}

describe("persisted recovery message failures", () => {
  it.each([
    { taskId: "other-task", messageId: "discarded" },
    { taskId: "task", messageId: "other-message" },
  ])("requires both task and message identity: %j", (reference) => {
    render(transcript({ failedMessages: [reference] }));
    expect(screen.queryByText(NOTE)).not.toBeInTheDocument();
  });

  it("marks the rest of the discarded turn, but not earlier history or another task", () => {
    render(transcript({ failedMessages: [{ taskId: "task", messageId: "discarded" }] }));
    expect(message("discarded")).toHaveAttribute("data-discarded", "message");
    expect(message("partial")).toHaveAttribute("data-discarded", "reply");
    expect(within(message("partial")).getByText(/Not remembered after recovery/)).toBeInTheDocument();
    expect(message("captured-answer")).not.toHaveAttribute("data-discarded");
    expect(message("next-turn")).not.toHaveAttribute("data-discarded");
  });

  it("does not announce persisted notes as a live region", () => {
    render(transcript({ failedMessages: [{ taskId: "task", messageId: "discarded" }] }));
    expect(within(message("discarded")).getByText(NOTE).closest("[role=status],[aria-live]")).toBeNull();
  });

  it("retains the failure indicator in a read-only shared transcript", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatTranscript chat={chat} recovery={{ failedMessages: [{ taskId: "task", messageId: "discarded" }] }} readOnly />
      </ThemeProvider>,
    );
    expect(within(message("discarded")).getByText(NOTE)).toBeInTheDocument();
  });

  it("hides a discarded task's stale question while retaining a captured question", () => {
    const pending: ChatController = { ...chat, turnState: "input_required", turnPhase: "awaiting_input", pendingQuestion: { kind: "unknown", taskId: "task" } };
    const view = render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatTranscript chat={pending} recovery={{ needed: false, failedMessages: [{ taskId: "task", messageId: "discarded" }] }} />
      </ThemeProvider>,
    );
    expect(screen.queryByTestId("chat-awaiting-reply")).not.toBeInTheDocument();
    expect(screen.queryByText("Waiting for you")).not.toBeInTheDocument();
    view.rerender(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatTranscript chat={pending} recovery={{ needed: false, failedMessages: [] }} />
      </ThemeProvider>,
    );
    expect(screen.getByTestId("chat-awaiting-reply")).toBeInTheDocument();
  });

  it("marks the exact discarded message after refresh and reload, preserving captured HITL history", () => {
    const recovery = { failedMessages: [{ taskId: "task", messageId: "discarded" }] };
    const view = render(transcript({ failedMessages: [] }));
    view.rerender(transcript(recovery));
    expect(within(message("discarded")).getByText(NOTE)).toHaveTextContent("the agent does not remember this message or the replies after it");
    expect(within(message("captured-answer")).queryByText(NOTE)).not.toBeInTheDocument();
    expect(screen.getAllByTestId("chat-message")).toHaveLength(4);
    view.unmount();
    render(transcript(recovery));
    expect(within(message("discarded")).getByText(NOTE)).toBeInTheDocument();
    expect(screen.getAllByTestId("chat-message")).toHaveLength(4);
  });
});
