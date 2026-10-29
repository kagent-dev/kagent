import { ThemeProvider } from "@emotion/react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstanceRecovery, ChatController } from "@/api";
import { ChatTranscript } from "./ChatTranscript";

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
    render(transcript({ automatic: false, failedMessages: [reference] }));
    expect(screen.queryByText(/Failed to send/)).not.toBeInTheDocument();
  });

  it("retains the failure indicator in a read-only shared transcript", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatTranscript chat={chat} recovery={{ automatic: false, failedMessages: [{ taskId: "task", messageId: "discarded" }] }} readOnly />
      </ThemeProvider>,
    );
    expect(within(message("discarded")).getByText(/Failed to send/)).toBeInTheDocument();
  });

  it("hides a discarded task's stale question while retaining a captured question", () => {
    const pending: ChatController = { ...chat, turnState: "input_required", turnPhase: "awaiting_input", pendingQuestion: { kind: "unknown", taskId: "task" } };
    const view = render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatTranscript chat={pending} recovery={{ needed: false, automatic: false, failedMessages: [{ taskId: "task", messageId: "discarded" }] }} />
      </ThemeProvider>,
    );
    expect(screen.queryByTestId("chat-awaiting-reply")).not.toBeInTheDocument();
    expect(screen.queryByText("Waiting for you")).not.toBeInTheDocument();
    view.rerender(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatTranscript chat={pending} recovery={{ needed: false, automatic: false, failedMessages: [] }} />
      </ThemeProvider>,
    );
    expect(screen.getByTestId("chat-awaiting-reply")).toBeInTheDocument();
  });

  it("marks the exact discarded message after refresh and reload, preserving captured HITL history", () => {
    const recovery = { automatic: false, failedMessages: [{ taskId: "task", messageId: "discarded" }] };
    const view = render(transcript({ automatic: false, failedMessages: [] }));
    view.rerender(transcript(recovery));
    expect(within(message("discarded")).getByText(/Failed to send/)).toHaveTextContent("the recovered agent did not retain this message");
    expect(within(message("captured-answer")).queryByText(/Failed to send/)).not.toBeInTheDocument();
    expect(screen.getAllByTestId("chat-message")).toHaveLength(2);
    view.unmount();
    render(transcript(recovery));
    expect(within(message("discarded")).getByText(/Failed to send/)).toBeInTheDocument();
    expect(screen.getAllByTestId("chat-message")).toHaveLength(2);
  });
});
