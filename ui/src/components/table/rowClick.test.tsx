import { fireEvent, render, screen } from "@testing-library/react";
import { createPortal } from "react-dom";
import { describe, expect, it, vi } from "vitest";
import { clickableRow } from "./rowClick";

describe("clickableRow", () => {
  it("ignores a click inside portalled content, which bubbles through React into the row", () => {
    const activate = vi.fn();
    render(
      <div onClick={clickableRow(activate).onClick}>
        {createPortal(<span data-testid="portalled">modal body</span>, document.body)}
      </div>,
    );
    fireEvent.click(screen.getByTestId("portalled"));
    expect(activate).not.toHaveBeenCalled();
  });

  it("ignores a click on a control in the row", () => {
    const activate = vi.fn();
    render(
      <div onClick={clickableRow(activate).onClick}>
        <button data-testid="control">go</button>
      </div>,
    );
    fireEvent.click(screen.getByTestId("control"));
    expect(activate).not.toHaveBeenCalled();
  });

  it("activates on a plain click", () => {
    const activate = vi.fn();
    render(<div onClick={clickableRow(activate).onClick}><span data-testid="cell">x</span></div>);
    fireEvent.click(screen.getByTestId("cell"));
    expect(activate).toHaveBeenCalledOnce();
  });
});
