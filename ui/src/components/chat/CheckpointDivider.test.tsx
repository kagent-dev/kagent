import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { Checkpoint, CheckpointState } from "@/api";
import { CheckpointDivider } from "./CheckpointDivider";

function show(state?: CheckpointState) {
  const checkpoint: Checkpoint | undefined = state
    ? { id: "saved", name: "Saved", agentInstanceId: "session", headTaskId: "task", state }
    : undefined;
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <CheckpointDivider checkpointId="saved" checkpoint={checkpoint} onFork={vi.fn()} onRename={vi.fn()} />
    </ThemeProvider>,
  );
}

describe("forking from a snapshot line", () => {
  it("offers Fork on a ready snapshot", () => {
    show("ready");
    expect(screen.getByTestId("chat-checkpoint-fork-saved")).toBeInTheDocument();
  });

  it.each<CheckpointState | undefined>([undefined, "creating", "failed", "deleting", "unknown"])(
    "does not offer Fork when the snapshot is %s",
    (state) => {
      show(state);
      expect(screen.queryByTestId("chat-checkpoint-fork-saved")).not.toBeInTheDocument();
      expect(screen.getByTestId("chat-checkpoint-rename-saved")).toBeInTheDocument();
    },
  );
});
