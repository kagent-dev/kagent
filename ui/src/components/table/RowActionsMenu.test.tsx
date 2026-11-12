import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { RowActionsMenu, copyPath, copySource } from "./RowActionsMenu";

function Where() {
  const { pathname, search } = useLocation();
  return <div data-testid="where">{pathname + search}</div>;
}

function renderMenu(onDelete = vi.fn().mockResolvedValue(undefined)) {
  render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <MemoryRouter>
        <RowActionsMenu
          kind="agent"
          namespace="ns"
          name="web"
          editTo="/agents/ns/web/edit"
          copyTo={copyPath("/agents/new", { namespace: "ns", name: "web" })}
          remove={{ onDelete, onDeleted: () => {} }}
        />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </ConfigProvider>,
  );
  fireEvent.click(screen.getByTestId("row-actions-ns/web"));
  return onDelete;
}

describe("copyPath", () => {
  it("encodes the source as namespace/name", () => {
    expect(copyPath("/agents/new", { namespace: "ns", name: "web" })).toBe("/agents/new?from=ns%2Fweb");
  });
});

describe("copySource", () => {
  it.each([
    ["ns/web", { namespace: "ns", name: "web" }],
    ["web", undefined],
    ["ns/", undefined],
    ["/web", undefined],
    ["a/b/c", undefined],
    ["", undefined],
  ])("reads %j", (from, want) => {
    expect(copySource(new URLSearchParams({ from }))).toEqual(want);
  });
  it("is no source when absent", () => {
    expect(copySource(new URLSearchParams())).toBeUndefined();
  });
});

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

describe("RowActionsMenu with delete unavailable", () => {
  it("disables Delete and says why on hover", async () => {
    render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <MemoryRouter>
          <RowActionsMenu
            kind="agent"
            namespace="ns"
            name="web"
            editTo="/e"
            copyTo="/c"
            disabledReason="Still in use"
            remove={{ onDelete: vi.fn(), onDeleted: () => {}, disabled: true }}
          />
        </MemoryRouter>
      </ConfigProvider>,
    );
    fireEvent.click(screen.getByTestId("row-actions-ns/web"));
    const item = await screen.findByTestId("row-delete-ns/web");
    expect(item).toHaveAttribute("aria-disabled", "true");
    fireEvent.mouseEnter(screen.getByText("Delete agent"));
    expect(await screen.findByText("Still in use")).toBeInTheDocument();
  });
});
