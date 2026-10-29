import { ThemeProvider } from "@emotion/react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstanceRecovery, Checkpoint } from "@/api";
import { SessionRecoveryAlert } from "./SessionRecoveryAlert";

const checkpoints: Checkpoint[] = [
  { id: "ready", name: "Saved conversation", agentInstanceId: "session", headTaskId: "task", state: "ready" },
  { id: "creating", name: "Still saving", agentInstanceId: "session", headTaskId: "task", state: "creating" },
];

function show(recovery?: AgentInstanceRecovery, isRecovering = false) {
  const onRevert = vi.fn();
  const onFork = vi.fn();
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <SessionRecoveryAlert {...{ recovery, checkpoints, isRecovering, onRevert, onFork }} />
    </ThemeProvider>,
  );
  return { onRevert, onFork };
}

describe("conversation recovery choices", () => {
  it.each([undefined, false])("does not offer recovery when the runtime observation is %s", (needed) => {
    show({ needed, automatic: false, failedMessages: [] });
    expect(screen.queryByRole("button", { name: /Revert/ })).not.toBeInTheDocument();
    expect(screen.queryByText("The agent runtime crashed")).not.toBeInTheDocument();
  });

  it("offers explicit revert in manual mode", async () => {
    const { onRevert } = show({ needed: true, automatic: false, failedMessages: [] });
    await userEvent.click(screen.getByRole("button", { name: "Revert to last snapshot" }));
    expect(onRevert).toHaveBeenCalledOnce();
  });

  it("forks only a ready saved snapshot through the supplied callback", async () => {
    const { onFork } = show({ needed: true, automatic: false, failedMessages: [] });
    expect(screen.getByRole("button", { name: "Fork snapshot" })).toBeDisabled();
    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Saved snapshot to fork" }));
    await userEvent.click(await screen.findByText("Saved conversation"));
    expect(screen.queryByText("Still saving")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Fork snapshot" }));
    expect(onFork).toHaveBeenCalledWith("ready");
  });

  it("prevents duplicate forks while the selected snapshot is opening", async () => {
    const { onFork } = show({ needed: true, automatic: false, failedMessages: [] });
    let finish = () => {};
    onFork.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
    fireEvent.mouseDown(screen.getByRole("combobox"));
    await userEvent.click(await screen.findByText("Saved conversation"));
    await userEvent.click(screen.getByRole("button", { name: "Fork snapshot" }));
    expect(screen.getByRole("button", { name: /Fork snapshot/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Revert to last snapshot" })).toBeDisabled();
    expect(onFork).toHaveBeenCalledOnce();
    await act(async () => finish());
  });

  it("shows automatic recovery progress without manual actions", () => {
    show({ needed: true, automatic: true, failedMessages: [] });
    expect(screen.getByText("Recovering this conversation")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("prevents another manual request while recovery is running", () => {
    show({ needed: true, automatic: false, failedMessages: [] }, true);
    expect(screen.getByRole("button", { name: /Revert to last snapshot/ })).toBeDisabled();
    expect(screen.getByRole("combobox")).toBeDisabled();
  });
});
