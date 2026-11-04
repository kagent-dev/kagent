import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstanceRecovery, Checkpoint } from "@/api";
import { SessionRecoveryAlert } from "./SessionRecoveryAlert";

const ready: Checkpoint = { id: "ready", name: "Saved conversation", agentInstanceId: "session", headTaskId: "task", state: "ready" };
const failed: Checkpoint = { id: "failed", name: "Not saved", agentInstanceId: "session", headTaskId: "task", state: "failed" };

function show(recovery?: AgentInstanceRecovery, { isRecovering = false, checkpoints = [ready] } = {}) {
  const onRevert = vi.fn();
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <SessionRecoveryAlert {...{ recovery, checkpoints, isRecovering, onRevert }} />
    </ThemeProvider>,
  );
  return { onRevert };
}

describe("conversation recovery choices", () => {
  it.each([undefined, false])("does not offer recovery when the runtime observation is %s", (needed) => {
    show({ needed, failedMessages: [] });
    expect(screen.queryByRole("button", { name: /Revert/ })).not.toBeInTheDocument();
    expect(screen.queryByText("The agent runtime crashed")).not.toBeInTheDocument();
  });

  it("offers explicit revert in manual mode", async () => {
    const { onRevert } = show({ needed: true, failedMessages: [] });
    await userEvent.click(screen.getByRole("button", { name: "Revert to last snapshot" }));
    expect(onRevert).toHaveBeenCalledOnce();
  });

  it("leaves forking to the snapshot lines in the transcript", () => {
    show({ needed: true, failedMessages: [] });
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.getByText(/fork a saved snapshot from the transcript/)).toBeInTheDocument();
  });

  it.each([
    { name: "none", checkpoints: [] },
    { name: "none ready", checkpoints: [failed] },
  ])("does not mention saved snapshots when there are $name", ({ checkpoints }) => {
    show({ needed: true, failedMessages: [] }, { checkpoints });
    expect(screen.getByText(/Revert to the last runtime snapshot/)).toBeInTheDocument();
    expect(screen.queryByText(/saved snapshot/)).not.toBeInTheDocument();
  });

  it("prevents another manual request while recovery is running", () => {
    show({ needed: true, failedMessages: [] }, { isRecovering: true });
    expect(screen.getByRole("button", { name: /Revert to last snapshot/ })).toBeDisabled();
    expect(screen.getByText("Recovering this conversation")).toBeInTheDocument();
  });
});
