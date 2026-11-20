import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { NameInput } from "./NameInput";

function Harness({ dots }: { dots: boolean }) {
  const [value, setValue] = useState("");
  return <NameInput dots={dots} data-testid="name" value={value} onChange={setValue} />;
}

describe("NameInput", () => {
  it("rewrites typed text to an object name's characters", async () => {
    render(<Harness dots />);
    await userEvent.type(screen.getByTestId("name"), " My Agent_v1.2!");
    expect(screen.getByTestId("name")).toHaveValue("my-agentv1.2");
  });

  it("drops dots where the rule has none, and trims pasted text", async () => {
    render(<Harness dots={false} />);
    await userEvent.click(screen.getByTestId("name"));
    await userEvent.paste("  Team.A B  ");
    expect(screen.getByTestId("name")).toHaveValue("teama-b");
  });

  it("keeps the caret where the user is typing", async () => {
    render(<Harness dots />);
    const input = screen.getByTestId<HTMLInputElement>("name");
    await userEvent.type(input, "ab");
    input.setSelectionRange(1, 1);
    await userEvent.keyboard("X ");
    expect(input).toHaveValue("ax-b");
    expect(input.selectionStart).toBe(3);
  });
});
