"use client";

import type React from "react";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { cn } from "@/lib/utils";

interface ChatMinimapProps {
  /** Ref to the ScrollArea root that wraps the Radix viewport. */
  containerRef: React.RefObject<HTMLDivElement | null>;
  /**
   * Bumped by the parent whenever the message list changes, so the minimap
   * re-binds its observers/listeners to the (possibly new) viewport. Height
   * changes from streaming are picked up by the ResizeObserver regardless.
   */
  revision: number;
}

interface Segment {
  topPct: number;
  heightPct: number;
  role: "user" | "assistant";
}

/**
 * A scrollbar-style minimap rendered on the right edge of the chat. Each message
 * becomes a segment sized proportionally to its height and colored by role; a
 * viewport indicator shows the current position. Click or drag the track to jump
 * through the history quickly.
 */
export default function ChatMinimap({ containerRef, revision }: ChatMinimapProps) {
  const viewportId = useId();
  const [segments, setSegments] = useState<Segment[]>([]);
  const [view, setView] = useState({ topPct: 0, heightPct: 100 });
  const [scrollable, setScrollable] = useState(false);
  const trackRef = useRef<HTMLDivElement>(null);
  const draggingRef = useRef(false);

  const getViewport = useCallback(
    () => (containerRef.current?.querySelector("[data-radix-scroll-area-viewport]") as HTMLElement | null) ?? null,
    [containerRef],
  );

  const updateView = useCallback(() => {
    const vp = getViewport();
    if (!vp) return;
    const total = vp.scrollHeight || 1;
    setView({
      topPct: (vp.scrollTop / total) * 100,
      heightPct: Math.min(100, (vp.clientHeight / total) * 100),
    });
  }, [getViewport]);

  const measure = useCallback(() => {
    const vp = getViewport();
    if (!vp) return;
    const total = vp.scrollHeight;
    if (total <= 0) return;

    const vpRect = vp.getBoundingClientRect();
    const items = Array.from(vp.querySelectorAll("[data-mm-item]")) as HTMLElement[];
    const segs: Segment[] = items.map((el) => {
      const r = el.getBoundingClientRect();
      const top = r.top - vpRect.top + vp.scrollTop;
      return {
        topPct: (top / total) * 100,
        heightPct: (r.height / total) * 100,
        role: el.getAttribute("data-mm-role") === "user" ? "user" : "assistant",
      };
    });

    setSegments(segs);
    setScrollable(total > vp.clientHeight + 4);
    updateView();
  }, [getViewport, updateView]);

  useEffect(() => {
    const vp = getViewport();
    if (!vp) return;
    if (!vp.id) {
      vp.id = viewportId;
    }

    // Defer the initial measurement so its setState calls don't run
    // synchronously inside the effect (which triggers cascading renders).
    // The ResizeObserver below also fires on observe(), so layout is captured
    // promptly regardless.
    const initialMeasure = requestAnimationFrame(() => measure());

    const onScroll = () => updateView();
    vp.addEventListener("scroll", onScroll, { passive: true });

    const ro = new ResizeObserver(() => measure());
    ro.observe(vp);
    const content = vp.firstElementChild;
    if (content) ro.observe(content);

    return () => {
      cancelAnimationFrame(initialMeasure);
      vp.removeEventListener("scroll", onScroll);
      ro.disconnect();
    };
  }, [getViewport, measure, updateView, revision, viewportId]);

  const scrollToClientY = useCallback(
    (clientY: number) => {
      const vp = getViewport();
      const track = trackRef.current;
      if (!vp || !track) return;
      const rect = track.getBoundingClientRect();
      const frac = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height));
      const total = vp.scrollHeight;
      // Center the viewport window on the clicked point for intuitive jumps.
      const target = frac * total - vp.clientHeight / 2;
      vp.scrollTo({ top: Math.max(0, target), behavior: draggingRef.current ? "auto" : "smooth" });
    },
    [getViewport],
  );

  const onPointerDown = (e: React.PointerEvent) => {
    draggingRef.current = true;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    scrollToClientY(e.clientY);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    if (!draggingRef.current) return;
    scrollToClientY(e.clientY);
  };
  const endDrag = (e: React.PointerEvent) => {
    draggingRef.current = false;
    e.currentTarget.releasePointerCapture?.(e.pointerId);
  };

  if (!scrollable || segments.length === 0) return null;

  return (
    <div className="absolute right-1 top-14 bottom-14 z-20 hidden w-2.5 md:block">
      <div
        ref={trackRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        className="group relative h-full w-full cursor-pointer rounded-full bg-muted/30 transition-colors hover:bg-muted/50"
        role="scrollbar"
        aria-label="Chat history minimap"
        aria-orientation="vertical"
        aria-controls={viewportId}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(view.topPct)}
      >
        {segments.map((s, i) => (
          <div
            key={i}
            className={cn(
              "pointer-events-none absolute left-1/2 -translate-x-1/2 rounded-sm",
              s.role === "user" ? "w-2 bg-primary/70" : "w-1.5 bg-muted-foreground/40",
            )}
            style={{ top: `${s.topPct}%`, height: `max(2px, calc(${s.heightPct}% - 1px))` }}
          />
        ))}
        <div
          className="pointer-events-none absolute left-0 right-0 rounded-md border border-primary/70 bg-primary/10"
          style={{ top: `${view.topPct}%`, height: `${view.heightPct}%` }}
        />
      </div>
    </div>
  );
}
