import { ThemeProvider } from "@emotion/react";
import type { ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { fireEvent, render as rtlRender, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DataPill, DataPillList } from "./DataPill";

const render = (ui: ReactElement) =>
  rtlRender(<ThemeProvider theme={themeFor("dark")}>{ui}</ThemeProvider>);

describe("DataPill", () => {
  it("shows its value with no remove button unless asked for one", () => {
    render(<DataPill data-testid="pill">api.example.com</DataPill>);
    expect(screen.getByTestId("pill")).toHaveTextContent("api.example.com");
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("is one button that removes the pill, from a click or the keyboard", async () => {
    const onRemove = vi.fn();
    render(<DataPill onRemove={onRemove} data-testid="pill">X-Team</DataPill>);
    const pill = screen.getByRole("button", { name: "Remove X-Team" });
    expect(pill).toBe(screen.getByTestId("pill"));
    fireEvent.click(screen.getByText("X-Team"));
    pill.focus();
    await userEvent.keyboard("{Enter}");
    await userEvent.keyboard(" ");
    expect(onRemove).toHaveBeenCalledTimes(3);
  });

  it("does not remove while disabled", () => {
    const onRemove = vi.fn();
    render(<DataPill onRemove={onRemove} disabled>X-Team</DataPill>);
    fireEvent.click(screen.getByRole("button", { name: "Remove X-Team" }));
    expect(onRemove).not.toHaveBeenCalled();
  });
});

describe("DataPillList", () => {
  it("renders one pill per item and removes the one clicked", () => {
    const onRemove = vi.fn();
    render(<DataPillList items={["a", "b"]} onRemove={onRemove} data-testid="pills" />);
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Remove b" }));
    expect(onRemove).toHaveBeenCalledWith("b");
  });

  it("shows the empty content when there are no items", () => {
    render(<DataPillList items={[]} empty="None yet" />);
    expect(screen.getByText("None yet")).toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });
});
