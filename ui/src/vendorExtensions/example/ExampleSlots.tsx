import { Alert, Button, Tag } from "antd";
import { useTheme } from "@emotion/react";
import { ShieldCheck } from "lucide-react";
import type { ExtensionPointProps } from "@/vendorExtensions";

/** Mounted inline at the top of the content area on every page. */
export function ExamplePolicyBanner() {
  const theme = useTheme();

  return (
    <Alert
      type="info"
      showIcon
      icon={<ShieldCheck size={16} />}
      data-testid="example-policy-banner"
      css={{ marginBottom: theme.space(5) }}
      title="Example policy engine is enforcing 3 guardrails on this cluster."
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
        boxShadow: "0 8px 24px rgba(0,0,0,0.45)",
        fontSize: 13,
        color: theme.color.text,
      }}
    >
      <span
        css={{
          width: 8,
          height: 8,
          borderRadius: "50%",
          background: theme.color.success,
        }}
      />
      Example agent mesh: healthy
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

/** Mounted inline in the Agents page header, once that page is rebuilt. */
export function ExampleAgentsHeaderAction() {
  return (
    <Button size="small" data-testid="example-agents-scan">
      Run Example scan
    </Button>
  );
}

/**
 * Mounted inline per agent row. Shows a point that carries context: the badge
 * needs to know which agent it is decorating.
 */
/** Mounted as the first card in the dashboard's summary grid. */
export function ExampleDashboardCard() {
  const theme = useTheme();

  return (
    <div
      data-testid="example-dashboard-card"
      css={{
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.lg,
        padding: theme.space(4),
      }}
    >
      <div css={{ color: theme.color.textMuted, marginBottom: theme.space(2) }}>
        Example compliance
      </div>
      <div css={{ fontSize: 24, fontWeight: 600 }}>3 guardrails</div>
    </div>
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
      title={`Send ${text.length} characters to Example review`}
    >
      Review
    </Button>
  );
}

export function ExampleAgentBadge({
  agentName,
  namespace,
}: ExtensionPointProps<"app_agents_agentsList_agentListItem_badge">) {
  return (
    <Tag color="purple" data-testid={`example-agent-badge-${namespace}-${agentName}`}>
      Example managed
    </Tag>
  );
}
