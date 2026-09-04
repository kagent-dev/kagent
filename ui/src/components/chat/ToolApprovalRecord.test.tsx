import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ToolApprovalRecord } from "./ToolApprovalRecord";

describe("ToolApprovalRecord", () => {
  it("renders decisions as statuses and keeps arguments collapsed", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolApprovalRecord
          part={{
            kind: "tool_approval",
            approval: {
              tools: [{ id: "approval-1", name: "delete_pod", args: { name: "old" } }],
              decisions: [{ id: "approval-1", approved: true }],
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText("Tool access approved")).toBeTruthy();
    expect(screen.getByText("delete_pod")).toBeTruthy();
    expect(screen.getByText("Approved")).toBeTruthy();
    expect(screen.getByTestId("chat-approval-record-args").closest("details")?.open).toBe(false);
    expect(screen.queryByText("Approved: delete_pod")).toBeNull();
  });

  it("keeps a rejection reason with the completed decision", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolApprovalRecord
          part={{
            kind: "tool_approval",
            approval: {
              tools: [{ id: "approval-1", name: "delete_pod", args: {} }],
              decisions: [
                {
                  id: "approval-1",
                  approved: false,
                  rejectionReason: "Production is still serving traffic",
                },
              ],
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText("Tool access rejected")).toBeTruthy();
    expect(screen.getByText("Production is still serving traffic")).toBeTruthy();
  });
});
