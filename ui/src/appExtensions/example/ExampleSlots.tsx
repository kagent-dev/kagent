import { Alert, Button, Tag } from "antd";
import { useTheme } from "@emotion/react";
import { Puzzle } from "lucide-react";
import { StatTile } from "@/components/dashboard/StatTile";
import { EXTENSION_POINT_IDS } from "@/appExtensions";
import type { ExtensionPointProps } from "@/appExtensions";

/**
 * What this extension mounts at each point.
 *
 * Every one of these says what it is rather than pretending to be a product
 * feature. That is deliberate: the example exists so a reader can see which
 * pixels on a page came from an extension and which came from the application,
 * and an invented domain would only make that harder to tell.
 *
 * They are drawn with the application's own components — `Alert`, `Button`,
 * `Tag`, and the dashboard's `StatTile` — because a contribution that wants to
 * belong on the page should look like it does. Nothing here needs custom styling
 * to sit correctly.
 */

/** Mounted inline at the top of the content area on every page. */
export function ExampleBanner() {
  const theme = useTheme();

  return (
    <Alert
      type="info"
      showIcon
      icon={<Puzzle size={16} />}
      data-testid="example-banner"
      css={{ marginBottom: theme.space(5) }}
      title="Example App Extension is installed. Everything labelled “Example” comes from it."
    />
  );
}

/**
 * Mounted at the portal point. Fixed to the viewport corner, which is exactly
 * why that point portals: declared inline it would be clipped by the content
 * area's scroll box instead of floating above the app.
 */
export function ExampleOverlayWidget() {
  const theme = useTheme();

  return (
    <div
      data-testid="example-overlay-widget"
      css={{
        position: "fixed",
        right: theme.space(6),
        bottom: theme.space(6),
        zIndex: 1000,
        display: "flex",
        alignItems: "center",
        gap: theme.space(2),
        padding: `${theme.space(2)} ${theme.space(4)}`,
        borderRadius: theme.radius.lg,
        border: `1px solid ${theme.color.border}`,
        background: theme.color.bgElevated,
        boxShadow: "0 8px 24px rgba(0, 0, 0, 0.25)",
        fontSize: 13,
        color: theme.color.textMuted,
      }}
    >
      <Puzzle size={14} />
      Example overlay
    </div>
  );
}

/** Mounted inline beneath the sidebar navigation. */
export function ExampleSidebarFooter() {
  const theme = useTheme();

  return (
    <div
      data-testid="example-sidebar-footer"
      css={{
        margin: theme.space(3),
        padding: theme.space(3),
        borderTop: `1px solid ${theme.color.border}`,
        fontSize: 12,
        color: theme.color.textMuted,
      }}
    >
      Extended by Example
    </div>
  );
}

/** Mounted inline in the Agents page header, beside the application's own buttons. */
export function ExampleAgentsHeaderAction() {
  return (
    <Button size="small" data-testid="example-agents-action">
      Example action
    </Button>
  );
}

/**
 * Mounted as the first card in the dashboard's summary grid.
 *
 * Rendered with the dashboard's own `StatTile`, so it matches the tiles beside it
 * exactly — the same border, radius, label colour and figure size — and keeps
 * matching them when any of those change.
 */
export function ExampleDashboardCard() {
  return (
    <StatTile
      label="Example"
      value={EXTENSION_POINT_IDS.length}
      hint="extension points offered"
      testId="example-dashboard-card"
    />
  );
}

/**
 * Mounted on every chat message. Demonstrates the richest context any point
 * passes — which message, who sent it, and what it said — so a contribution can
 * act on one turn rather than on the conversation as a whole.
 */
export function ExampleMessageAction({
  messageId,
  role,
  text,
}: ExtensionPointProps<"app_agents_agentChat_agentChatMessage_additionalActionsButton">) {
  return (
    <Button
      size="small"
      type="text"
      data-testid={`example-message-action-${role}-${messageId}`}
      // Reads the turn's own text, so the contribution demonstrably receives
      // content and not just identifiers.
      title={`This ${role} message is ${text.length} characters long`}
    >
      Example
    </Button>
  );
}

/**
 * Mounted inline per agent row. Shows a point that carries context: the badge
 * needs to know which agent it is decorating.
 */
export function ExampleAgentBadge({
  agentName,
  namespace,
}: ExtensionPointProps<"app_agents_agentsList_agentListItem_badge">) {
  return (
    <Tag data-testid={`example-agent-badge-${namespace}-${agentName}`}>Example</Tag>
  );
}
