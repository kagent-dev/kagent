import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { AskUserPrompt } from "./AskUserPrompt";

describe("AskUserPrompt tool approval", () => {
  it("shows the invocation and returns an approval for its opaque id", () => {
    const onToolApproval = vi.fn();
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <AskUserPrompt
          request={{
            kind: "tool_approval",
            taskId: "task-1",
            hint: "This reads resources from the cluster.",
            tools: [
              {
                id: "approval-7",
                name: "kagent-tool-server.k8s_get_resources",
                args: { namespace: "default", kind: "Pod" },
              },
            ],
          }}
          isBusy={false}
          onAnswer={vi.fn()}
          onToolApproval={onToolApproval}
          onDismiss={vi.fn()}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText("kagent-tool-server.k8s_get_resources")).toBeTruthy();
    expect(screen.getByText(/"namespace": "default"/)).toBeTruthy();

    fireEvent.click(screen.getByTestId("chat-approval-approve"));

    expect(onToolApproval).toHaveBeenCalledWith([
      { id: "approval-7", approved: true },
    ]);
  });

  it("collects an optional reason before sending a rejection", () => {
    const onToolApproval = vi.fn();
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <AskUserPrompt
          request={{
            kind: "tool_approval",
            taskId: "task-1",
            tools: [{ id: "approval-7", name: "delete_pod", args: {} }],
          }}
          isBusy={false}
          onAnswer={vi.fn()}
          onToolApproval={onToolApproval}
          onDismiss={vi.fn()}
        />
      </ThemeProvider>,
    );

    fireEvent.click(screen.getByTestId("chat-approval-reject"));
    expect(onToolApproval).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Reason for rejecting delete_pod"), {
      target: { value: "The production pod is still serving traffic" },
    });
    fireEvent.click(screen.getByTestId("chat-approval-submit"));

    expect(onToolApproval).toHaveBeenCalledWith([
      {
        id: "approval-7",
        approved: false,
        rejectionReason: "The production pod is still serving traffic",
      },
    ]);
  });

  it("collects an independent decision for every concurrent invocation", () => {
    const onToolApproval = vi.fn();
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <AskUserPrompt
          request={{
            kind: "tool_approval",
            taskId: "task-1",
            tools: [
              { id: "approval-1", name: "read", args: {} },
              { id: "approval-2", name: "write", args: {} },
            ],
          }}
          isBusy={false}
          onAnswer={vi.fn()}
          onToolApproval={onToolApproval}
          onDismiss={vi.fn()}
        />
      </ThemeProvider>,
    );

    const allow = screen.getAllByRole("button", { name: /allow/i });
    const deny = screen.getAllByRole("button", { name: /deny/i });
    fireEvent.click(allow[0]);
    fireEvent.click(deny[1]);
    fireEvent.click(screen.getByTestId("chat-approval-submit"));

    expect(onToolApproval).toHaveBeenCalledWith([
      { id: "approval-1", approved: true },
      { id: "approval-2", approved: false },
    ]);
  });
});
