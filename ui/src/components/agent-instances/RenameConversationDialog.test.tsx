import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { ConfigProvider } from "antd";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstance } from "@/api";

const { rename, invalidate, toastSaved } = vi.hoisted(() => ({
  rename: vi.fn(),
  invalidate: vi.fn(),
  toastSaved: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { agentInstances: { rename } },
  useInvalidateConversations: () => invalidate,
}));
vi.mock("@/components/form/saveToast", () => ({ toastSaved }));

import { RenameConversationDialog } from "./RenameConversationDialog";

const instance = { id: "6f1c9d20-0000-0000-0000-000000000000", name: "" } as AgentInstance;

function renderDialog(onClose = vi.fn()) {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <RenameConversationDialog instance={instance} onClose={onClose} />
      </ConfigProvider>
    </ThemeProvider>,
  );
  return onClose;
}

describe("RenameConversationDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    rename.mockResolvedValue(undefined);
    invalidate.mockResolvedValue(undefined);
  });

  it("labels the field and marks it optional", () => {
    renderDialog();
    expect(screen.getByLabelText(/^Conversation name/)).toBeInTheDocument();
    expect(screen.getByTestId("field-optional")).toBeInTheDocument();
  });

  it("shows an inline error on an invalid submit and sends nothing", async () => {
    renderDialog();
    await userEvent.type(screen.getByLabelText(/^Conversation name/), " padded");
    expect(screen.queryByTestId("conversation-rename-problem")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByTestId("conversation-rename-problem")).toHaveTextContent("cannot start or end with a space");
    expect(rename).not.toHaveBeenCalled();
  });

  it("saves a valid name, shows the shared toast and closes", async () => {
    const onClose = renderDialog();
    await userEvent.type(screen.getByLabelText(/^Conversation name/), "Rollback");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(rename).toHaveBeenCalledWith(instance.id, "Rollback");
    expect(toastSaved).toHaveBeenCalledWith("saved", "conversation name", "Rollback");
  });

  it("shows the shared error and keeps the dialog open when the write fails", async () => {
    rename.mockRejectedValue(new Error("boom"));
    const onClose = renderDialog();
    await userEvent.type(screen.getByLabelText(/^Conversation name/), "Rollback");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByTestId("conversation-rename-error")).toHaveTextContent("The conversation name was not saved");
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
