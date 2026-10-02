import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { AppExtensionsProvider } from "@/appExtensions";
import type { ChatPartRendererProps } from "@/appExtensions";
import type { ChatMessage } from "@/api";
import { ChatMessageItem } from "./ChatMessageItem";

describe("ChatMessageItem interaction layout", () => {
  it("renders a JSON DataPart as the agent's structured result, not a tool result", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ChatMessageItem
          message={{
            id: "answer-1",
            role: "agent",
            createdAt: "2026-09-03T00:00:00Z",
            parts: [
              {
                kind: "data",
                dataKind: "structured_output",
                data: { status: "success", payload: { customerId: "12345" } },
                mediaType: "application/json",
                metadata: { "kagent.dev/a2a/output-schema-sha256": "abc123456789" },
              },
            ],
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByTestId("chat-structured-output")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
    expect(screen.queryByTestId("chat-tool-result")).not.toBeInTheDocument();
  });

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

describe("ChatMessageItem part renderers", () => {
  const message: ChatMessage = {
    id: "answer-1",
    role: "agent",
    taskId: "task-1",
    createdAt: "2026-09-03T00:00:00Z",
    parts: [
      { kind: "text", text: "Hello **there**" },
      { kind: "data", dataKind: "tool_call", data: { name: "lookup", id: "call_1", args: {} } },
    ],
  };

  function renderMessage(ui: React.ReactNode) {
    return render(<ThemeProvider theme={themeFor("dark")}>{ui}</ThemeProvider>);
  }

  it("uses an extension's renderer for its key and the core one for the rest", () => {
    const TextRenderer = ({ part, role, messageId, taskId, sessionId }: ChatPartRendererProps<"text">) => (
      <p data-testid="custom-text">{[part.text, role, messageId, taskId, sessionId].join("|")}</p>
    );

    renderMessage(
      <AppExtensionsProvider
        extensions={[{ id: "x", name: "X", chatPartRenderers: { text: TextRenderer } }]}
      >
        <ChatMessageItem message={message} sessionId="session-1" />
      </AppExtensionsProvider>,
    );

    expect(screen.getByTestId("custom-text")).toHaveTextContent(
      "Hello **there**|agent|answer-1|task-1|session-1",
    );
    expect(screen.queryByTestId("chat-message-text")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-tool-call")).toBeInTheDocument();
  });

  it("renders the core bubble when no extension replaces text", () => {
    renderMessage(<ChatMessageItem message={message} />);

    expect(screen.getByTestId("chat-message-text")).toContainHTML("<strong>there</strong>");
  });

  it("never hands an empty text part to an extension renderer", () => {
    const TextRenderer = () => <p data-testid="custom-text" />;
    renderMessage(
      <AppExtensionsProvider
        extensions={[{ id: "x", name: "X", chatPartRenderers: { text: TextRenderer } }]}
      >
        <ChatMessageItem message={{ ...message, parts: [{ kind: "text", text: "" }] }} />
      </AppExtensionsProvider>,
    );

    expect(screen.queryByTestId("custom-text")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-message-pending")).toBeInTheDocument();
  });
});
