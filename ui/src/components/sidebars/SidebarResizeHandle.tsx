"use client";

import React, { useCallback } from "react";

interface SidebarResizeHandleProps {
  /** Which side the sidebar sits on; determines drag math and handle edge. */
  side: "left" | "right";
  onResize: (width: number) => void;
  /** Double-click resets to the default width. */
  onReset: () => void;
}

/**
 * Invisible drag strip on a sidebar's inner edge. Dragging resizes the
 * sidebar (via onResize with the new px width); double-click resets.
 */
export default function SidebarResizeHandle({ side, onResize, onReset }: SidebarResizeHandleProps) {
  const handlePointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      const target = e.currentTarget;
      target.setPointerCapture(e.pointerId);

      const onMove = (event: PointerEvent) => {
        const next = side === "left" ? event.clientX : window.innerWidth - event.clientX;
        onResize(next);
      };
      const onUp = (event: PointerEvent) => {
        target.releasePointerCapture(event.pointerId);
        target.removeEventListener("pointermove", onMove);
        target.removeEventListener("pointerup", onUp);
      };
      target.addEventListener("pointermove", onMove);
      target.addEventListener("pointerup", onUp);
    },
    [side, onResize]
  );

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      title="Drag to resize · double-click to reset"
      onPointerDown={handlePointerDown}
      onDoubleClick={onReset}
      className={`absolute inset-y-0 z-20 w-1.5 cursor-col-resize touch-none transition-colors hover:bg-primary/20 active:bg-primary/30 ${
        side === "left" ? "right-0" : "left-0"
      }`}
    />
  );
}
