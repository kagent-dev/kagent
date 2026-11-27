import { ThemeProvider } from "@emotion/react";
import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { KeyValueList } from "./KeyValueList";

function Headers({ error }: { error?: string }) {
  const [rows, setRows] = useState([{ key: "X-Team", value: "infra" }]);
  return (
    <ThemeProvider theme={themeFor("light")}>
      <KeyValueList
        data-testid="headers"
        rows={rows}
        keyTitle="Header name"
        valueTitle="Value"
        keyPlaceholder="Header name"
        valuePlaceholder="Value"
        addLabel="Add header"
        onAdd={() => setRows([...rows, { key: "", value: "" }])}
        onChange={(index, patch) => setRows(rows.map((row, at) => (at === index ? { ...row, ...patch } : row)))}
        onRemove={(index) => setRows(rows.filter((_, at) => at !== index))}
        removeLabel={(index) => `Remove header ${rows[index].key || index + 1}`}
        keyField={(index) => ({ error: index === 0 ? error : undefined, onTouch: () => undefined })}
      />
    </ThemeProvider>
  );
}

describe("KeyValueList", () => {
  it("shows column titles once, not a title per row", () => {
    render(<Headers />);
    expect(screen.getAllByText("Header name")).toHaveLength(1);
    expect(screen.getByRole("textbox", { name: "Header name 1" })).toHaveValue("X-Team");
    expect(screen.getByRole("textbox", { name: "Value 1" })).toHaveAttribute("placeholder", "Value");
  });

  it("focuses an added row and moves focus to Add after a remove", async () => {
    const user = userEvent.setup();
    render(<Headers />);
    await user.click(screen.getByRole("button", { name: "Add header" }));
    expect(screen.getByRole("textbox", { name: "Header name 2" })).toHaveFocus();
    expect(screen.getByRole("textbox", { name: "Header name 2" })).toHaveAttribute("placeholder", "Header name");

    await user.click(screen.getByRole("button", { name: "Remove header X-Team" }));
    expect(screen.getAllByTestId("headers-row")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Add header" })).toHaveFocus();
  });

  it("shows a key's error under it and marks it invalid", () => {
    render(<Headers error="Enter a key." />);
    const key = screen.getByRole("textbox", { name: "Header name 1" });
    expect(key).toHaveAttribute("aria-invalid", "true");
    expect(key).toHaveAccessibleDescription("Enter a key.");
  });
});
