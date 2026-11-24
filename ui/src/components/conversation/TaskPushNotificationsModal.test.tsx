import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { ConfigProvider } from "antd";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import type { ConversationRef } from "@/api/conversation/types";

const { listTaskPushConfigs, saveTaskPushConfig, deleteTaskPushConfig, taskAcceptsPushConfigs, toastSaved } = vi.hoisted(() => ({
  listTaskPushConfigs: vi.fn(),
  saveTaskPushConfig: vi.fn(),
  deleteTaskPushConfig: vi.fn(),
  taskAcceptsPushConfigs: vi.fn(),
  toastSaved: vi.fn(),
}));

vi.mock("@/api/conversation/taskPushConfigs", () => ({
  listTaskPushConfigs,
  saveTaskPushConfig,
  deleteTaskPushConfig,
  taskAcceptsPushConfigs,
}));
vi.mock("@/components/form/saveToast", () => ({ toastSaved }));

import { TaskPushNotificationsModal } from "./TaskPushNotificationsModal";

const conversation = { id: "c1" } as ConversationRef;
const existing = { id: "hook-1", url: "https://example.com/old" };

function renderModal() {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <TaskPushNotificationsModal conversation={conversation} taskId="t1" onClose={vi.fn()} />
      </ConfigProvider>
    </ThemeProvider>,
  );
}

describe("TaskPushNotificationsModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listTaskPushConfigs.mockResolvedValue([existing]);
    taskAcceptsPushConfigs.mockResolvedValue(true);
    saveTaskPushConfig.mockResolvedValue(undefined);
  });

  it("labels both fields, with the URL required and the ID optional", async () => {
    renderModal();
    expect(await screen.findByLabelText(/^Callback URL/)).toBeInTheDocument();
    expect(screen.getByLabelText(/^Callback ID/)).toBeInTheDocument();
    expect(screen.getAllByTestId("field-required")).toHaveLength(1);
    expect(screen.getAllByTestId("field-optional")).toHaveLength(1);
  });

  it("flags an invalid URL inline as it is typed and sends nothing", async () => {
    renderModal();
    const field = await screen.findByLabelText(/^Callback URL/);
    expect(field).not.toHaveAttribute("aria-invalid");
    await userEvent.type(field, "not a url");
    expect(screen.getByText(/absolute HTTP or HTTPS/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add callback" }));
    expect(saveTaskPushConfig).not.toHaveBeenCalled();
  });

  it("saves a valid callback and shows the shared toast", async () => {
    renderModal();
    await userEvent.type(await screen.findByLabelText(/^Callback URL/), "https://example.com/new");
    fireEvent.click(screen.getByRole("button", { name: "Add callback" }));

    await waitFor(() => expect(toastSaved).toHaveBeenCalledWith("saved", "task callback", "https://example.com/new"));
    expect(saveTaskPushConfig).toHaveBeenCalledWith(conversation, "t1", { id: "", url: "https://example.com/new" });
  });

  it("cancels an edit and clears the form", async () => {
    renderModal();
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    expect(screen.getByLabelText(/^Callback URL/)).toHaveValue(existing.url);

    fireEvent.click(screen.getByRole("button", { name: "Cancel edit" }));

    expect(screen.getByLabelText(/^Callback URL/)).toHaveValue("");
    expect(saveTaskPushConfig).not.toHaveBeenCalled();
  });

  it("sends one request when Enter follows Add callback", async () => {
    saveTaskPushConfig.mockReturnValue(new Promise(() => {}));
    renderModal();
    const field = await screen.findByLabelText(/^Callback URL/);
    await userEvent.type(field, "https://example.com/new");
    fireEvent.click(screen.getByRole("button", { name: "Add callback" }));
    fireEvent.keyDown(field, { key: "Enter", code: "Enter", keyCode: 13 });
    expect(saveTaskPushConfig).toHaveBeenCalledTimes(1);
  });
});
