import { ThemeProvider } from "@emotion/react";
import { useState, type ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { fireEvent, render as rtlRender, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { FormRow, FormRowList } from "./FormRowList";

const render = (ui: ReactElement) =>
  rtlRender(<ThemeProvider theme={themeFor("dark")}>{ui}</ThemeProvider>);

function Rows({ manageFocus, canAdd = true }: { manageFocus?: boolean; canAdd?: boolean }) {
  const [rows, setRows] = useState(["first", "second"]);
  return (
    <FormRowList
      data-testid="rows"
      addLabel="Add header"
      onAdd={canAdd ? () => setRows((current) => [...current, `row-${current.length + 1}`]) : undefined}
      empty="No headers"
      manageFocus={manageFocus}
      actions={canAdd ? <button type="button">Pick one</button> : undefined}
    >
      {rows.map((row) => (
        <FormRow key={row} removeLabel={`Remove ${row}`} onRemove={() => setRows((current) => current.filter((r) => r !== row))}>
          <input aria-label={`Value ${row}`} />
          {row}
        </FormRow>
      ))}
    </FormRowList>
  );
}

describe("FormRowList", () => {
  it("adds and removes rows", () => {
    render(<Rows />);
    fireEvent.click(screen.getByTestId("rows-add"));
    expect(screen.getAllByRole("listitem").map((row) => row.textContent)).toEqual(["first", "second", "row-3"]);

    fireEvent.click(screen.getByRole("button", { name: "Remove second" }));
    expect(screen.getAllByRole("listitem").map((row) => row.textContent)).toEqual(["first", "row-3"]);
  });

  it("separates every row with a divider", () => {
    render(<Rows />);
    const list = within(screen.getByTestId("rows")).getByRole("list");
    expect(getComputedStyle(list).borderTopStyle).toBe("solid");
    for (const row of screen.getAllByRole("listitem")) {
      expect(getComputedStyle(row).borderBottomStyle).toBe("solid");
    }
  });

  it("shows the empty content once the last row is removed", () => {
    render(<Rows />);
    fireEvent.click(screen.getByRole("button", { name: "Remove first" }));
    fireEvent.click(screen.getByRole("button", { name: "Remove second" }));
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
    expect(screen.getByText("No headers")).toBeInTheDocument();
  });

  it("shows extra add controls beside the add button", () => {
    render(<Rows />);
    expect(screen.getByRole("button", { name: "Pick one" })).toBeInTheDocument();
  });

  it("moves focus into an added row, and to the add button after a remove", () => {
    render(<Rows manageFocus />);
    fireEvent.click(screen.getByTestId("rows-add"));
    expect(screen.getByRole("textbox", { name: "Value row-3" })).toHaveFocus();

    fireEvent.click(screen.getByRole("button", { name: "Remove first" }));
    expect(screen.getByTestId("rows-add")).toHaveFocus();
  });

  it("leaves focus on the add button after an add unless asked", () => {
    render(<Rows />);
    fireEvent.click(screen.getByTestId("rows-add"));
    expect(screen.getByRole("textbox", { name: "Value row-3" })).not.toHaveFocus();
  });

  it("moves focus to the add button when a row is removed by keyboard", async () => {
    render(<Rows />);
    screen.getByRole("button", { name: "Remove second" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByTestId("rows-add")).toHaveFocus();
  });

  it("moves focus to the previous row without an add button, then to the list", async () => {
    render(<Rows canAdd={false} />);
    screen.getByRole("button", { name: "Remove second" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByRole("textbox", { name: "Value first" })).toHaveFocus();

    screen.getByRole("button", { name: "Remove first" }).focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByTestId("rows")).toHaveFocus();
  });
});
