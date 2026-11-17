import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { ConfigProvider } from "antd";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { AgentInstance } from "@/api";

const { rename, invalidate, toastSaved, toastSuccess } = vi.hoisted(() => ({
  rename: vi.fn(),
  invalidate: vi.fn(),
  toastSaved: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { agentInstances: { rename } },
  useInvalidateConversations: () => invalidate,
}));
vi.mock("@/components/form/saveToast", () => ({ toastSaved, toastSuccess }));

import { RenameConversationDialog } from "./RenameConversationDialog";

const instance = { id: "6f1c9d20-0000-0000-0000-000000000000", name: "" } as AgentInstance;

function renderDialog(onClose = vi.fn(), target: AgentInstance = instance) {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <RenameConversationDialog instance={target} onClose={onClose} />
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
    expect(screen.getByLabelText(/^Conversation name/)).not.toHaveAccessibleDescription();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const field = screen.getByLabelText(/^Conversation name/);
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription(/cannot start or end with a space/);
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

  it("sends one request when Enter follows Save", async () => {
    rename.mockReturnValue(new Promise(() => {}));
    renderDialog();
    const field = screen.getByLabelText(/^Conversation name/);
    await userEvent.type(field, "Rollback");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    fireEvent.keyDown(field, { key: "Enter", code: "Enter", keyCode: 13 });
    expect(rename).toHaveBeenCalledTimes(1);
  });

  it("says the name was cleared when saved empty", async () => {
    const onClose = renderDialog(vi.fn(), { ...instance, name: "Rollback" });
    await userEvent.clear(screen.getByLabelText(/^Conversation name/));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(rename).toHaveBeenCalledWith(instance.id, "");
    expect(toastSuccess).toHaveBeenCalledWith("Cleared the name of conversation 6f1c9d20");
    expect(toastSaved).not.toHaveBeenCalled();
  });
});
