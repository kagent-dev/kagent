import { ThemeProvider } from "@emotion/react";
import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ListInput } from "./ListInput";
import { scanListText, type ListInputSplit } from "./listInputText";

describe("scanListText", () => {
  it.each<[string, ListInputSplit, string[], string | undefined]>([
    ["node server.js", "spaces", ["node"], "server.js"],
    ["--a=b,c ", "spaces", ["--a=b,c"], undefined],
    ['-c "echo hi" next', "spaces", ["-c", "echo hi"], "next"],
    ['say "half open', "spaces", ["say"], "half open"],
    ["tools, billing,api", "commasAndSpaces", ["tools", "billing"], "api"],
    [" , ,", "commasAndSpaces", [], undefined],
  ])("splits %j by %s", (text, split, items, trailing) => {
    const scan = scanListText(text, split);
    expect(scan.items).toEqual(items);
    expect(scan.trailing?.value).toBe(trailing);
  });
});

function Field({ split, unique }: { split: ListInputSplit; unique?: boolean }) {
  const [value, setValue] = useState<string[]>([]);
  return (
    <ThemeProvider theme={themeFor("light")}>
      <ListInput split={split} unique={unique} value={value} onChange={setValue} data-testid="list" />
      <output data-testid="value">{JSON.stringify(value)}</output>
    </ThemeProvider>
  );
}

const items = () => within(screen.getByTestId("list")).queryAllByRole("listitem").map((item) => item.textContent);

describe("ListInput", () => {
  it("adds, splits, pastes and removes items, keeping repeats in argv order", async () => {
    const user = userEvent.setup();
    render(<Field split="spaces" />);
    const input = screen.getByTestId("list-input");
    expect(screen.getByTestId("list-add")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Add" })).toBe(screen.getByTestId("list-add"));
    expect(screen.getByTestId("list-add")).toHaveTextContent("");

    await user.type(input, "node -v -v");
    expect(items()).toEqual(["node", "-v"]);
    await user.click(screen.getByTestId("list-add"));
    expect(items()).toEqual(["node", "-v", "-v"]);
    expect(input).toHaveValue("");

    await user.click(input);
    await user.paste('--msg "two words"');
    expect(items()).toEqual(["node", "-v", "-v", "--msg", "two words"]);

    await user.type(input, "last{Enter}");
    expect(screen.getByTestId("value")).toHaveTextContent('["node","-v","-v","--msg","two words","last"]');

    await user.click(screen.getByRole("button", { name: "Remove two words" }));
    await user.type(input, "{Backspace}");
    expect(items()).toEqual(["node", "-v", "-v", "--msg"]);
  });

  it("splits on commas and drops repeats where the list is a set", async () => {
    const user = userEvent.setup();
    render(<Field split="commasAndSpaces" unique />);
    await user.type(screen.getByTestId("list-input"), "tools, billing,tools{Enter}");
    expect(items()).toEqual(["tools", "billing"]);
  });
});
