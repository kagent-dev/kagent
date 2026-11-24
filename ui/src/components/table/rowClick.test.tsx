import { fireEvent, render, screen } from "@testing-library/react";
import { createPortal } from "react-dom";
import { describe, expect, it, vi } from "vitest";
import { clickableRow, linkRow } from "./rowClick";

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

describe("linkRow", () => {
  function renderRow(expand?: () => void) {
    const followed = vi.fn((event: Event) => event.preventDefault());
    render(
      <table>
        <tbody>
          <tr {...linkRow({ expand })} data-testid="row">
            <td className="ant-table-row-expand-icon-cell" data-testid="expand-cell" />
            <td>
              <a href="/agents/a" data-row-link="" onClick={(event) => followed(event.nativeEvent)}>a</a>
            </td>
            <td data-testid="cell">x</td>
            <td><button data-testid="actions">…</button></td>
          </tr>
        </tbody>
      </table>,
    );
    return followed;
  }

  it("follows the row's link on a plain click anywhere in the row", () => {
    const followed = renderRow();
    fireEvent.click(screen.getByTestId("cell"));
    expect(followed).toHaveBeenCalledOnce();
  });

  it.each([{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }])(
    "opens the link in a new tab on a modified click (%o)",
    (modifier) => {
      const open = vi.spyOn(window, "open").mockReturnValue(null);
      const followed = renderRow();
      fireEvent.click(screen.getByTestId("cell"), modifier);
      expect(open).toHaveBeenCalledWith(expect.stringMatching(/\/agents\/a$/), "_blank", "noopener");
      expect(followed).not.toHaveBeenCalled();
      open.mockRestore();
    },
  );

  it("opens the link in a new tab on a middle click", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderRow();
    fireEvent(screen.getByTestId("cell"), new MouseEvent("auxclick", { bubbles: true, button: 1 }));
    expect(open).toHaveBeenCalledOnce();
    open.mockRestore();
  });

  it("leaves the row's own controls alone", () => {
    const followed = renderRow();
    fireEvent.click(screen.getByTestId("actions"));
    expect(followed).not.toHaveBeenCalled();
  });

  it("expands instead of following the link when the expand cell is clicked", () => {
    const expand = vi.fn();
    const followed = renderRow(expand);
    fireEvent.click(screen.getByTestId("expand-cell"));
    expect(expand).toHaveBeenCalledOnce();
    expect(followed).not.toHaveBeenCalled();
  });
});
