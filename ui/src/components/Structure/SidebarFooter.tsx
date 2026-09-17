import { Tooltip } from "antd";
import { useTheme } from "@emotion/react";
import {
  BookOpen,
  ExternalLink,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Sun,
} from "lucide-react";
import { useThemeMode } from "@/theme/themeMode";

/** Where the docs link points. The project's own documentation, not a deployment's. */
const DOCS_URL = "https://kagent.dev/docs/kagent";

/**
 * The controls at the foot of the sidebar.
 *
 * Styled as navigation rather than as buttons, because that is what they are next
 * to: a row here should read as a sibling of "Agents", not as a toolbar that
 * happens to sit below one. So they share the nav row's shape — the same inset, the
 * same radius, the same hover — and differ only in what they do.
 *
 * Collapsed, each reduces to its icon with the label as a tooltip. A row whose text
 * is clipped mid-word is worse than one that is plainly an icon.
 */
export function SidebarFooter({
  collapsed,
  onToggleCollapsed,
}: {
  collapsed: boolean;
  onToggleCollapsed: () => void;
}) {
  const theme = useTheme();
  const { mode, toggle, canToggle } = useThemeMode();

  const row = {
    display: "flex",
    alignItems: "center",
    // Matched to the component library's own menu item, measured rather than assumed:
    // it insets its icon 24px from the row's edge and leaves 10px before the label. The
    // row boxes lining up is not enough on its own — with this row's own 12px inset the
    // boxes agreed and every glyph still sat 12px to the left of the navigation above.
    gap: 10,
    marginBlock: 2,
    // The 24px only applies while there is a label to line up with. On the collapsed
    // rail it leaves 4px of content box for a 16px icon, which renders each of these as
    // a speck; collapsed, the row just centres its icon like the navigation does.
    padding: collapsed
      ? `${theme.space(2)} ${theme.space(2)}`
      : `${theme.space(2)} ${theme.space(6)}`,
    // Full width of the rail's inset, which the container supplies as padding rather
    // than each row as a margin. That is what makes these three the same width: a
    // `<button>` shrinks to fit its content even as a flex container, while the `<a>`
    // beside it stretched — so with the inset on the rows, Documentation spanned the
    // rail and the two buttons under it were each as wide as their own label.
    width: "100%",
    boxSizing: "border-box" as const,
    border: "none",
    background: "transparent",
    borderRadius: theme.radius.md,
    color: theme.color.textMuted,
    font: "inherit",
    fontSize: 14,
    textAlign: "left" as const,
    cursor: "pointer",
    justifyContent: collapsed ? "center" : "flex-start",
    transition: "background 120ms ease, color 120ms ease",
    "&:hover": { background: theme.color.border, color: theme.color.text },
    "&:focus-visible": {
      outline: `2px solid ${theme.color.primary}`,
      outlineOffset: -2,
    },
  };

  const label = (text: string) =>
    collapsed ? null : <span css={{ whiteSpace: "nowrap" }}>{text}</span>;

  return (
    <div
      data-testid="sidebar-footer"
      css={{
        marginTop: "auto",
        paddingBottom: theme.space(2),
        // Separated from the navigation above it rather than floating: these are
        // about the application, not about where you are in it.
        borderTop: `1px solid ${theme.color.border}`,
        paddingTop: theme.space(2),
        // The same inset the nav rows take as a margin, so the two line up down the
        // left edge while the rows themselves stay full width.
        paddingInline: theme.space(2),
      }}
    >
      <Tooltip title={collapsed ? "Documentation" : undefined} placement="right">
        <a
          href={DOCS_URL}
          target="_blank"
          // `noreferrer` as well as `noopener`: this is an outbound link from a
          // console whose URL can name a cluster.
          rel="noopener noreferrer"
          data-testid="sidebar-docs"
          css={row}
        >
          <BookOpen size={16} aria-hidden />
          {/* The label and its mark as one run of text, so the icon reads as part of the
              word rather than as a control at the other end of the row.
              
              It is here because this row leaves the product and the other two do not:
              without it the row is indistinguishable from navigation, and it opens a new
              tab — a reader who did not expect that has to work out where their console
              went. `aria-hidden` because the anchor's own name already carries it. */}
          {collapsed ? null : (
            <span
              css={{
                display: "inline-flex",
                alignItems: "center",
                gap: 5,
                whiteSpace: "nowrap",
              }}
            >
              Documentation
              <ExternalLink size={12} aria-hidden css={{ opacity: 0.7 }} />
            </span>
          )}
        </a>
      </Tooltip>

      {/* Only where there is another palette to switch to. An extension whose
          components can only be read on a dark page says so, and a toggle that
          cannot change anything reads as broken. */}
      {canToggle ? (
      <Tooltip
        title={collapsed ? `Switch to ${mode === "dark" ? "light" : "dark"} theme` : undefined}
        placement="right"
      >
        <button
          type="button"
          onClick={toggle}
          data-testid="theme-toggle"
          // The label says what pressing it does, not what is currently true — a
          // toggle announced as its own state tells a screen reader the opposite of
          // what it is about to do.
          aria-label={`Switch to ${mode === "dark" ? "light" : "dark"} theme`}
          css={row}
        >
          {mode === "dark" ? <Sun size={16} aria-hidden /> : <Moon size={16} aria-hidden />}
          {label(mode === "dark" ? "Light theme" : "Dark theme")}
        </button>
      </Tooltip>
      ) : null}

      <Tooltip title={collapsed ? "Expand sidebar" : undefined} placement="right">
        <button
          type="button"
          onClick={onToggleCollapsed}
          data-testid="sidebar-collapse"
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          css={row}
        >
          {collapsed ? (
            <PanelLeftOpen size={16} aria-hidden />
          ) : (
            <PanelLeftClose size={16} aria-hidden />
          )}
          {label("Collapse")}
        </button>
      </Tooltip>
    </div>
  );
}
