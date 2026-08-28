import { fireEvent, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ResizableAside } from "./ResizableAside";

/**
 * What a drag is allowed to do to the panel's width.
 *
 * The arithmetic is the part worth pinning: the panel is on the right, so it grows as the
 * pointer moves *left*, which reads as correct written either way round. The bounds
 * matter as much — a panel dragged past the window's edge, or down to nothing, leaves a
 * reader with no way back other than reloading.
 */

function renderAside() {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ResizableAside
        testId="aside"
        handleTestId="handle"
        label="Resize the panel"
        defaultWidth={300}
        minWidth={240}
        maxWidth={640}
      >
        <p>Panel</p>
      </ResizableAside>
    </ThemeProvider>,
  );

  return { aside: screen.getByTestId("aside"), handle: screen.getByTestId("handle") };
}

/**
 * A drag from `fromX` to `toX`, as the pointer would deliver it.
 *
 * Through `fireEvent` rather than `dispatchEvent`: the move handler sets state, and a
 * raw dispatch leaves React with an update it has not been told to flush — the width
 * then reads as whatever it was before the drag.
 */
function drag(handle: HTMLElement, fromX: number, toX: number) {
  fireEvent.pointerDown(handle, { clientX: fromX, pointerId: 1 });
  fireEvent.pointerMove(handle, { clientX: toX, pointerId: 1 });
  fireEvent.pointerUp(handle, { clientX: toX, pointerId: 1 });
}

describe("ResizableAside", () => {
  it("opens at the width it was given", () => {
    expect(renderAside().aside.style.width).toBe("300px");
  });

  it("widens as the pointer moves toward the conversation", () => {
    const { aside, handle } = renderAside();

    drag(handle, 900, 800);

    expect(aside.style.width).toBe("400px");
  });

  it("narrows as the pointer moves away from it", () => {
    const { aside, handle } = renderAside();

    drag(handle, 900, 940);

    expect(aside.style.width).toBe("260px");
  });

  it("will not be dragged narrower than its minimum", () => {
    const { aside, handle } = renderAside();

    drag(handle, 900, 1600);

    expect(aside.style.width).toBe("240px");
  });

  it("will not be dragged wider than its maximum", () => {
    const { aside, handle } = renderAside();

    drag(handle, 900, 100);

    expect(aside.style.width).toBe("640px");
  });

  // The drag ends when the pointer is released: a later move is somebody else's.
  it("stops following the pointer once released", () => {
    const { aside, handle } = renderAside();

    drag(handle, 900, 800);
    fireEvent.pointerMove(handle, { clientX: 400, pointerId: 1 });

    expect(aside.style.width).toBe("400px");
  });

  it("says what the handle is for", () => {
    renderAside();

    expect(screen.getByRole("separator", { name: "Resize the panel" })).toBeTruthy();
  });
});
