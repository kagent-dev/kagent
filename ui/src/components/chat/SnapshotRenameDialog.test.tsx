import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { ConfigProvider } from "antd";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { Checkpoint } from "@/api";

const { rename, onRenamed, toastSaved } = vi.hoisted(() => ({
  rename: vi.fn(),
  onRenamed: vi.fn(),
  toastSaved: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { agentInstances: { checkpoints: { rename } } },
}));
vi.mock("@/components/form/saveToast", () => ({ toastSaved }));

import { SnapshotRenameDialog } from "./SnapshotRenameDialog";

const checkpoint = { id: "cp-1", name: "" } as Checkpoint;

function renderDialog(onClose = vi.fn()) {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <SnapshotRenameDialog checkpoint={checkpoint} onClose={onClose} onRenamed={onRenamed} />
      </ConfigProvider>
    </ThemeProvider>,
  );
  return onClose;
}

describe("SnapshotRenameDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    rename.mockImplementation((_id: string, name: string) => Promise.resolve({ ...checkpoint, name }));
    onRenamed.mockResolvedValue(undefined);
  });

  it("labels the field and marks it optional", () => {
    renderDialog();
    expect(screen.getByLabelText(/^Snapshot name/)).toBeInTheDocument();
    expect(screen.getByTestId("field-optional")).toBeInTheDocument();
  });

  it("shows an inline error on an invalid submit and sends nothing", async () => {
    renderDialog();
    await userEvent.type(screen.getByLabelText(/^Snapshot name/), " padded");
    expect(screen.queryByTestId("snapshot-rename-problem")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByTestId("snapshot-rename-problem")).toHaveTextContent("cannot start or end with a space");
    expect(rename).not.toHaveBeenCalled();
  });

  it("saves a valid name, shows the shared toast and closes", async () => {
    const onClose = renderDialog();
    await userEvent.type(screen.getByLabelText(/^Snapshot name/), "Rollback");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(rename).toHaveBeenCalledWith(checkpoint.id, "Rollback");
    expect(toastSaved).toHaveBeenCalledWith("saved", "snapshot name", "Rollback");
    expect(onRenamed).toHaveBeenCalledWith({ ...checkpoint, name: "Rollback" });
  });

  it("shows the shared error and keeps the dialog open when the write fails", async () => {
    rename.mockRejectedValue(new Error("boom"));
    const onClose = renderDialog();
    await userEvent.type(screen.getByLabelText(/^Snapshot name/), "Rollback");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByTestId("snapshot-rename-error")).toHaveTextContent("The snapshot name was not saved");
    expect(onClose).not.toHaveBeenCalled();
    expect(toastSaved).not.toHaveBeenCalled();
  });

  it("closes without saving on cancel", () => {
    const onClose = renderDialog();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalled();
    expect(rename).not.toHaveBeenCalled();
  });
});
