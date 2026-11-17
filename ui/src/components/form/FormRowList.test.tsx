import { ThemeProvider } from "@emotion/react";
import { useState, type ReactElement } from "react";
import { themeFor } from "@/theme/theme";
import { fireEvent, render as rtlRender, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { FormRow, FormRowList } from "./FormRowList";

const render = (ui: ReactElement) =>
  rtlRender(<ThemeProvider theme={themeFor("dark")}>{ui}</ThemeProvider>);

function Rows() {
  const [rows, setRows] = useState(["first", "second"]);
  return (
    <FormRowList
      data-testid="rows"
      addLabel="Add header"
      onAdd={() => setRows((current) => [...current, `row-${current.length + 1}`])}
      empty="No headers"
    >
      {rows.map((row) => (
        <FormRow key={row} removeLabel={`Remove ${row}`} onRemove={() => setRows((current) => current.filter((r) => r !== row))}>
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
});
