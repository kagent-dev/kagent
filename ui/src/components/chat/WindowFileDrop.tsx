import { useEffect, useEffectEvent, useState } from "react";
import { createPortal } from "react-dom";
import { useTheme } from "@emotion/react";

const hasFiles = (event: DragEvent) => event.dataTransfer?.types.includes("Files") ?? false;

/**
 * Takes files dropped anywhere on the page while it is mounted.
 *
 * File drags are always claimed, even when `enabled` is false, so a stray drop
 * cannot navigate the tab to the file. Other drags are left alone.
 */
export function WindowFileDrop({
  enabled,
  onFiles,
}: {
  enabled: boolean;
  onFiles: (files: File[]) => void;
}) {
  const theme = useTheme();
  const [dragging, setDragging] = useState(false);
  const takeFiles = useEffectEvent(onFiles);

  useEffect(() => {
    // Counted because dragenter and dragleave fire for every child crossed.
    let depth = 0;
    const reset = () => {
      depth = 0;
      setDragging(false);
    };
    const onEnter = (event: DragEvent) => {
      if (!hasFiles(event) || !enabled) return;
      depth += 1;
      setDragging(true);
    };
    const onLeave = (event: DragEvent) => {
      if (!hasFiles(event) || !enabled) return;
      depth = Math.max(0, depth - 1);
      if (!depth) setDragging(false);
    };
    const onOver = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      if (!enabled && event.dataTransfer) event.dataTransfer.dropEffect = "none";
    };
    const onDrop = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      reset();
      if (enabled) takeFiles([...(event.dataTransfer?.files ?? [])]);
    };
    window.addEventListener("dragenter", onEnter);
    window.addEventListener("dragleave", onLeave);
    window.addEventListener("dragover", onOver);
    window.addEventListener("drop", onDrop);
    window.addEventListener("dragend", reset);
    return () => {
      window.removeEventListener("dragenter", onEnter);
      window.removeEventListener("dragleave", onLeave);
      window.removeEventListener("dragover", onOver);
      window.removeEventListener("drop", onDrop);
      window.removeEventListener("dragend", reset);
      setDragging(false);
    };
  }, [enabled]);

  if (!dragging) return null;
  return createPortal(
    <div
      data-testid="chat-drop-overlay"
      css={{
        position: "fixed",
        inset: 0,
        zIndex: 1000,
        // Drops land on the page underneath; the window listener takes them.
        pointerEvents: "none",
        display: "grid",
        placeItems: "center",
        padding: theme.space(6),
        background: `color-mix(in srgb, ${theme.color.bg} 80%, transparent)`,
      }}
    >
      <div
        css={{
          padding: `${theme.space(6)} ${theme.space(10)}`,
          border: `2px dashed ${theme.color.primaryText}`,
          borderRadius: theme.radius.lg,
          background: theme.color.bgElevated,
          color: theme.color.text,
          fontSize: 16,
          fontWeight: 600,
        }}
      >
        Drop files to attach
      </div>
    </div>,
    document.body,
  );
}
