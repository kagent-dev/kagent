import { useState, type ReactNode } from "react";
import { Button } from "antd";
import { PanelRightClose, PanelRightOpen } from "lucide-react";
import { useTheme } from "@emotion/react";
import { iconControlStyles } from "@/components/agent/controlStyles";
import { useCollapsedBelow } from "@/components/conversation/useNarrowViewport";

/**
 * The row's layout on every agent page: the gap between the rail, the gutter icons, the
 * content and the panel (10px from each icon with its button's 4px padding), and how far
 * the row reaches into the app's 24px content padding (to 8px).
 */
export const agentSurfaceRow = {
  gap: 6,
  marginInline: -16,
  // How far a collapsed rail or panel pulls its gutter icon outward: from 8px off the edge
  // to 10px, the same as on its inner side. Read by `AgentRail` as well as below.
  "--agent-gutter-collapsed-pull": "2px",
} as const;

/** Where the agent panel's open state is remembered, per reader, for every page that shows it. */
const CONTEXT_OPEN = "kagent.conversation.agentPanel.open";

/**
 * The window width below which the panel folds away on its own.
 *
 * It goes before the application sidebar (antd's `lg`) and before the rail, which is
 * the only way to the other conversations. `max-width` includes its edge, hence 1599.
 */
const CONTEXT_COLLAPSES_BELOW = 1599;

/** Wider when the window has room, so a `namespace/server` name fits on one line. */
const CONTEXT_WIDTH = "clamp(244px, calc(25vw - 28px), 340px)";

/**
 * The agent panel's column: a toggle, and the panel sliding open beside it.
 *
 * Shared by the agent, new-conversation and conversation pages, so its width,
 * breakpoint and remembered state are the same on all three.
 */
export function AgentContextAside({
  ready = true,
  unavailable = false,
  children,
}: {
  /** False while the panel's subject is still loading: the column keeps its place, the toggle hides. */
  ready?: boolean;
  /** True when there is nothing to show (the conversation could not be read), so no empty column is held. */
  unavailable?: boolean;
  children: ReactNode;
}) {
  const theme = useTheme();
  const [isNarrow, setNarrow] = useCollapsedBelow(CONTEXT_COLLAPSES_BELOW);
  // Absent means open: someone meeting an agent for the first time gets the context.
  const [wantsOpen, setWantsOpen] = useState(() => window.localStorage.getItem(CONTEXT_OPEN) !== "false");
  const isOpen = wantsOpen && !isNarrow;

  // Only the reader's choice is stored: a panel the window closed should reopen on the next wide session.
  function toggle() {
    const open = !isOpen;
    window.localStorage.setItem(CONTEXT_OPEN, String(open));
    setWantsOpen(open);
    if (open) setNarrow(false);
  }

  const sticky = {
    position: "sticky",
    top: `var(--agent-rail-sticky-top, ${theme.layout.headerHeight + 24}px)`,
    alignSelf: "start",
  } as const;

  return (
    <>
      <div
        css={{
          ...sticky,
          flexShrink: 0,
          // Hidden rather than absent, so the row does not shift when the subject loads.
          visibility: ready ? "visible" : "hidden",
        }}
      >
        <Button
          type="text"
          size="small"
          css={iconControlStyles(theme)}
          icon={isOpen ? <PanelRightClose size={16} aria-hidden /> : <PanelRightOpen size={16} aria-hidden />}
          onClick={toggle}
          aria-label={isOpen ? "Hide the agent panel" : "Show the agent panel"}
          data-testid={isOpen ? "conversation-context-collapse" : "conversation-context-expand"}
        />
      </div>

      {/* Slides rather than vanishing, so the content beside it does not jump its whole width in one frame. */}
      <div
        css={{
          ...sticky,
          flexShrink: 0,
          width: isOpen && !unavailable ? CONTEXT_WIDTH : 0,
          // Closed, it gives back the row gap before it too, so the toggle sits as far from the edge as the left one.
          marginInlineStart: isOpen && !unavailable ? 0 : -agentSurfaceRow.gap,
          marginInlineEnd: isOpen && !unavailable ? 0 : "calc(-1 * var(--agent-gutter-collapsed-pull, 0px))",
          overflow: "hidden",
          // Hidden for real once closed, after the width transition; immediate when opening.
          visibility: isOpen ? "visible" : "hidden",
          transition: `width 180ms ease, margin-inline-start 180ms ease, margin-inline-end 180ms ease, visibility 0s linear ${isOpen ? "0s" : "180ms"}`,
        }}
        aria-hidden={!isOpen}
      >
        <div
          data-testid="conversation-context-aside"
          css={{ width: CONTEXT_WIDTH, maxHeight: "calc(100vh - 160px)", overflowY: "auto" }}
        >
          {children}
        </div>
      </div>
    </>
  );
}
