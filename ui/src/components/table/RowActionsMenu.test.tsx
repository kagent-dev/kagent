import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { AppExtensionsProvider, type AppExtensionConfig, type ExtensionRowAction } from "@/appExtensions";
import { RowActionsMenu } from "./RowActionsMenu";
import { copyPath } from "./copyFrom";

function Where() {
  const { pathname, search } = useLocation();
  return <div data-testid="where">{pathname + search}</div>;
}

function renderMenu(onDelete = vi.fn().mockResolvedValue(undefined), extensions: AppExtensionConfig[] = []) {
  render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <AppExtensionsProvider extensions={extensions}>
        <MemoryRouter>
          <RowActionsMenu
            kind="agent"
            namespace="ns"
            name="web"
            editTo="/agents/ns/web/edit"
            copyTo={copyPath("/agents/new", { namespace: "ns", name: "web" })}
            extensionMenu="app_agents_agentsList_rowMenu"
            remove={{ onDelete, onDeleted: () => {} }}
          />
          <Routes>
            <Route path="*" element={<Where />} />
          </Routes>
        </MemoryRouter>
      </AppExtensionsProvider>
    </ConfigProvider>,
  );
  fireEvent.click(screen.getByTestId("row-actions-ns/web"));
  return onDelete;
}

describe("RowActionsMenu", () => {
  it("opens the edit form", async () => {
    renderMenu();
    fireEvent.click(await screen.findByTestId("row-edit-ns/web"));
    expect(screen.getByTestId("where")).toHaveTextContent("/agents/ns/web/edit");
  });

  it("opens the new form prefilled from this row", async () => {
    renderMenu();
    fireEvent.click(await screen.findByTestId("row-copy-ns/web"));
    expect(screen.getByTestId("where")).toHaveTextContent("/agents/new?from=ns%2Fweb");
  });

  const action = (over: Partial<ExtensionRowAction> = {}): AppExtensionConfig => ({
    id: "ext",
    name: "Ext",
    rowActions: [{ id: "trace", menuId: "app_agents_agentsList_rowMenu", label: "Trace", onClick: vi.fn(), ...over }],
  });

  it("runs an installed extension's action, told which row it is for", async () => {
    const onClick = vi.fn();
    renderMenu(undefined, [action({ onClick })]);
    fireEvent.click(await screen.findByText("Trace"));
    expect(onClick).toHaveBeenCalledExactlyOnceWith({ kind: "agent", namespace: "ns", name: "web" });
    expect(screen.getByTestId("where")).toHaveTextContent("/");
  });

  it("contains an extension action that fails, whether it throws, rejects or cannot be placed", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const menu = "app_agents_agentsList_rowMenu" as const;
    renderMenu(undefined, [{
      id: "ext",
      name: "Ext",
      rowActions: [
        { id: "bad", menuId: menu, label: "Bad", visible: () => { throw new Error("no visibility"); }, onClick: vi.fn() },
        { id: "boom", menuId: menu, label: "Boom", onClick: () => { throw new Error("boom"); } },
        { id: "reject", menuId: menu, label: "Reject", onClick: () => Promise.reject(new Error("rejected")) },
      ],
    }]);
    expect(screen.queryByText("Bad")).not.toBeInTheDocument();
    fireEvent.click(await screen.findByText("Boom"));
    fireEvent.click(await screen.findByText("Reject"));
    await waitFor(() => expect(error).toHaveBeenCalledWith(expect.stringContaining('"reject" failed'), expect.objectContaining({ message: "rejected" })));
    expect(error).toHaveBeenCalledWith(expect.stringContaining('"bad" crashed'), expect.objectContaining({ message: "no visibility" }));
    expect(error).toHaveBeenCalledWith(expect.stringContaining('"boom" failed'), expect.objectContaining({ message: "boom" }));
    error.mockRestore();
  });

  it("leaves an action out of a row it is not visible for", async () => {
    renderMenu(undefined, [action({ visible: ({ name }) => name !== "web" })]);
    await screen.findByTestId("row-edit-ns/web");
    expect(screen.queryByText("Trace")).not.toBeInTheDocument();
  });

  it("asks before deleting, and deletes only once confirmed", async () => {
    const onDelete = renderMenu();
    fireEvent.click(await screen.findByTestId("row-delete-ns/web"));
    expect(onDelete).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(onDelete).toHaveBeenCalledWith(false));
  });

  it("deletes nothing when the confirmation is dismissed", async () => {
    const onDelete = renderMenu();
    fireEvent.click(await screen.findByTestId("row-delete-ns/web"));
    fireEvent.click(await screen.findByRole("button", { name: "Keep" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Keep" })).not.toBeInTheDocument());
    expect(onDelete).not.toHaveBeenCalled();
  });
});
