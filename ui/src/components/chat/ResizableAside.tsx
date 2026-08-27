import { useState, type ReactNode } from "react";
import { useTheme } from "@emotion/react";

/**
 * A panel beside the conversation whose width the reader sets.
 *
 * ## Why it is draggable at all
 *
 * What goes in it is a list of somebody's tool names and skill descriptions, and those
 * are as long as they happen to be. No single width is right: wide enough for the worst
 * name wastes the conversation's room on every other agent, and narrow enough to be
 * polite truncates the names that are the reason to open the panel.
 *
 * ## Why the width is not remembered
 *
 * A reader who has dragged the panel to a quarter of the window and forgotten has no
 * obvious way back, and "close it and open it" is a simpler instruction than a reset
 * control that means nothing until you have already made a mess. Both chats mount this
 * only while the panel is open, so closing it is the way back.
 */
export function ResizableAside({
  children,
  testId,
  handleTestId,
  label,
  defaultWidth = 300,
  minWidth = 240,
  maxWidth = 640,
  maxHeight = "calc(100vh - 160px)",
}: {
  children: ReactNode;
  testId: string;
  handleTestId: string;
  /** What the handle is for, for anyone not looking at it. */
  label: string;
  defaultWidth?: number;
  minWidth?: number;
  maxWidth?: number;
  maxHeight?: string;
}) {
  const theme = useTheme();
  const [width, setWidth] = useState(defaultWidth);

  /**
   * Dragging the panel's edge.
   *
   * Pointer events rather than mouse events, so a trackpad and a touch screen behave the
   * same, and captured on the handle so the drag survives the cursor leaving it — without
   * capture, moving faster than React re-renders drops the drag halfway.
   *
   * The panel is on the right, so it grows as the pointer moves *left*: hence the
   * subtraction, which is the sort of thing that reads as correct and behaves backwards.
   */
  function startResize(event: React.PointerEvent<HTMLDivElement>) {
    event.preventDefault();
    const handle = event.currentTarget;
    const startX = event.clientX;
    const startWidth = width;
    // Optional because a test renderer has no pointer to capture. The drag still works
    // there — capture is what keeps a *fast* drag alive, not what starts it.
    handle.setPointerCapture?.(event.pointerId);

    const onMove = (move: PointerEvent) => {
      const next = startWidth + (startX - move.clientX);
      setWidth(Math.min(maxWidth, Math.max(minWidth, next)));
    };
    const onUp = () => {
      handle.releasePointerCapture?.(event.pointerId);
      handle.removeEventListener("pointermove", onMove);
      handle.removeEventListener("pointerup", onUp);
    };

    handle.addEventListener("pointermove", onMove);
    handle.addEventListener("pointerup", onUp);
  }

  return (
    /* Beside the conversation rather than over it: what the reader checks the tool list
       against is the exchange they have just read. Sticky, because the page is what
       scrolls. */
    <aside
      data-testid={testId}
      style={{ width }}
      css={{
        flexShrink: 0,
        paddingLeft: theme.space(5),
        borderLeft: `1px solid ${theme.color.border}`,
        alignSelf: "start",
        position: "sticky",
        top: theme.space(2),
        maxHeight,
        overflowY: "auto",
      }}
    >
      {/* The edge, draggable. The handle sits on the border because that is where a
          reader already aims, and shows itself only under the pointer: a permanent grip
          on a border is a line beside a line. */}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={label}
        data-testid={handleTestId}
        onPointerDown={startResize}
        css={{
          position: "absolute",
          inset: "0 auto 0 -3px",
          width: 7,
          cursor: "col-resize",
          "&:hover, &:active": {
            background: `color-mix(in srgb, ${theme.color.primary} 45%, transparent)`,
          },
        }}
      />
      {children}
    </aside>
  );
}
