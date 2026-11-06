import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { Checkpoint } from "@/api";
import { SessionRecoveryAlert } from "./SessionRecoveryAlert";

const ready: Checkpoint = { id: "ready", name: "Saved conversation", agentInstanceId: "session", headTaskId: "task", state: "ready" };
const failed: Checkpoint = { id: "failed", name: "Not saved", agentInstanceId: "session", headTaskId: "task", state: "failed" };

function show(needed?: boolean, { isRecovering = false, isReverting = false, checkpoints = [ready] } = {}) {
  const onRevert = vi.fn();
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <SessionRecoveryAlert {...{ needed, checkpoints, isRecovering, isReverting, onRevert }} />
    </ThemeProvider>,
  );
  return { onRevert };
}

describe("conversation recovery choices", () => {
  it.each([undefined, false])("does not offer recovery when the runtime observation is %s", (needed) => {
    show(needed);
    expect(screen.queryByRole("button", { name: /Revert/ })).not.toBeInTheDocument();
    expect(screen.queryByText("The agent runtime crashed")).not.toBeInTheDocument();
  });

  it("offers explicit revert in manual mode", async () => {
    const { onRevert } = show(true);
    await userEvent.click(screen.getByRole("button", { name: "Revert to last snapshot" }));
    expect(onRevert).toHaveBeenCalledOnce();
  });

  it("leaves forking to the snapshot lines in the transcript", () => {
    show(true);
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.getByText(/fork a saved snapshot from the transcript/)).toBeInTheDocument();
  });

  it.each([
    { name: "none", checkpoints: [] },
    { name: "none ready", checkpoints: [failed] },
  ])("does not mention saved snapshots when there are $name", ({ checkpoints }) => {
    show(true, { checkpoints });
    expect(screen.getByText(/Revert to the last runtime snapshot/)).toBeInTheDocument();
    expect(screen.queryByText(/saved snapshot/)).not.toBeInTheDocument();
  });

  it("keeps revert and fork available while an admitted recovery is pending", async () => {
    const { onRevert } = show(true, { isRecovering: true });
    expect(screen.getByText("Recovering this conversation")).toBeInTheDocument();
    expect(screen.queryByText("The agent runtime crashed")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Revert to last snapshot" }));
    expect(onRevert).toHaveBeenCalledOnce();
    // Forking stays on the transcript's snapshot lines, as in manual mode.
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.getByText(/fork a saved snapshot from the transcript/)).toBeInTheDocument();
    expect(screen.getByText(/does not remember messages sent after that snapshot/)).toBeInTheDocument();
    expect(screen.queryByText(/marked as failed/)).not.toBeInTheDocument();
  });

  /*
   * An admitted recovery keeps its operation through every failed attempt. Disabling
   * Revert for as long as one was admitted left the button spinning once the first
   * attempt failed, with nothing else to press.
   */
  it("keeps Revert usable while a recovery admitted from another tab is pending", async () => {
    const { onRevert } = show(true, { isRecovering: true });
    expect(screen.getByText("Recovering this conversation")).toBeInTheDocument();
    const revert = screen.getByRole("button", { name: "Revert to last snapshot" });
    expect(revert).toBeEnabled();
    await userEvent.click(revert);
    expect(onRevert).toHaveBeenCalledOnce();
  });

  it("disables Revert only while this page's revert is in flight", () => {
    const props = { needed: true, checkpoints: [ready], isRecovering: true, onRevert: vi.fn() };
    const { rerender } = render(
      <ThemeProvider theme={themeFor("dark")}>
        <SessionRecoveryAlert {...props} isReverting />
      </ThemeProvider>,
    );
    expect(screen.getByRole("button", { name: /Revert to last snapshot/ })).toBeDisabled();
    // The request failed; the admitted recovery is still pending, and so is the choice.
    rerender(
      <ThemeProvider theme={themeFor("dark")}>
        <SessionRecoveryAlert {...props} isReverting={false} />
      </ThemeProvider>,
    );
    expect(screen.getByRole("button", { name: /Revert to last snapshot/ })).toBeEnabled();
    expect(screen.getByText("Recovering this conversation")).toBeInTheDocument();
  });
});
