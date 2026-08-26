import { useMemo } from "react";
import { Alert, Button, Card, Empty, Space, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { formatDistanceToNow } from "date-fns";
import { Link } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ReadinessMeter } from "@/components/dashboard/ReadinessMeter";
import { StatTile } from "@/components/dashboard/StatTile";
import { ToolsPerServerChart } from "@/components/dashboard/ToolsPerServerChart";
import { VendorSlot } from "@/vendorExtensions";
import { buildPath, paths } from "@/router/routes";
import {
  useAgentInstancesAcrossNamespaces,
  useMcpServers,
  useModels,
  useNamespaces,
  useTools,
  type AgentInstance,
} from "@/api";
import { conversationTitle } from "@/components/agent-instances/instanceLabels";
import { useConversationTitles } from "@/api/hooks/useConversationTitles";
import { RefreshButton } from "@/components/table/RefreshButton";

const { Text } = Typography;

/** Most recently created first; agents the cluster gave no timestamp for sort last. */
function byNewest(a: AgentInstance, b: AgentInstance): number {
  const left = a.createdAt ?? "";
  const right = b.createdAt ?? "";
  if (left === right) return 0;
  if (!left) return 1;
  if (!right) return -1;
  return right.localeCompare(left);
}

/**
 * What the cluster currently holds, and what changed in it most recently.
 *
 * Four independent reads back this page, and they are deliberately *not* merged
 * into one all-or-nothing state: if the model list fails, the agent counts
 * already fetched are still true and still worth showing. Each figure reports
 * only what it knows, and the banner names exactly which reads failed.
 */
export function DashboardPage() {
  const theme = useTheme();

  /*
   * The agents, which are `AgentInstance`s, across every namespace the controller
   * watches.
   *
   * `AgentInstanceService` has no cross-namespace read, so this is a request per
   * namespace merged together — which is why the namespace list is read first and
   * this is held back until it arrives.
   */
  const namespaces = useNamespaces();
  const namespaceNames = useMemo(
    () => (namespaces.data ?? []).map((entry) => entry.name),
    [namespaces.data],
  );
  const agents = useAgentInstancesAcrossNamespaces(namespaceNames);
  const models = useModels();
  const servers = useMcpServers();
  const tools = useTools();

  const resources = [
    { label: "agents", resource: agents },
    { label: "model configurations", resource: models },
    { label: "MCP servers", resource: servers },
    { label: "tools", resource: tools },
  ];
  const failures = resources.filter((entry) => entry.resource.error);
  const isRefreshing = resources.some((entry) => entry.resource.isValidating);

  async function refreshAll(): Promise<void> {
    await Promise.all(resources.map((entry) => entry.resource.refresh()));
  }

  // Rows are dropped on failure for the same reason the lists drop them: a
  // failed read must not be rendered as "there is nothing here".
  const agentRows = agents.error ? [] : (agents.data?.instances ?? []);
  const readyCount = agentRows.filter((row) => row.state === "ready").length;
  const recent = [...agentRows].sort(byNewest).slice(0, 5);
  /*
   * Titles for the unnamed ones among those five.
   *
   * The hook reads one task list per conversation and only for the ones nobody has
   * named, so this is at most five reads and usually fewer — well inside the budget it
   * enforces for the rail, which does the same thing for thirty. Without it a card
   * headed "Recent agent conversations" lists rows called "Untitled", which is true and
   * useless.
   */
  const derivedTitles = useConversationTitles(recent);

  return (
    <PageFrame
      title="Dashboard"
      description="Overview of agents, models, and recent activity."
      actions={
        <RefreshButton onRefresh={refreshAll} what="Dashboard" loading={isRefreshing} />
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {failures.length > 0 ? (
          <Alert
            type="error"
            showIcon
            title="Some of this page could not be loaded"
            description={`Failed to read ${listNames(
              failures.map((entry) => entry.label),
            )}. Everything else here is current.`}
            data-testid="dashboard-error"
            action={
              <Button size="small" onClick={() => void refreshAll()}>
                Try again
              </Button>
            }
          />
        ) : null}

        <div
          data-testid="dashboard-summary-grid"
          css={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))",
            gap: theme.space(4),
          }}
        >
          {/* Ahead of the application's own figures, so a distribution can lead
              with something of its own without displacing any of them. */}
          <VendorSlot id="app_dashboard_dashboardOverview_summaryGrid_leadingCard" />

          <StatTile
            label="Agents"
            testId="stat-agents"
            value={agents.error ? undefined : agents.data?.instances.length}
            isLoading={agents.isLoading}
            hint={agentsHint(
              agents.error !== undefined,
              agents.data?.instances,
              readyCount,
            )}
          />
          <StatTile
            label="Model configurations"
            testId="stat-models"
            value={models.error ? undefined : models.data?.length}
            isLoading={models.isLoading}
            hint={models.error ? UNREADABLE : undefined}
          />
          <StatTile
            label="MCP servers"
            testId="stat-mcp-servers"
            value={servers.error ? undefined : servers.data?.length}
            isLoading={servers.isLoading}
            hint={servers.error ? UNREADABLE : undefined}
          />
          <StatTile
            label="Tools discovered"
            testId="stat-tools"
            value={tools.error ? undefined : tools.data?.length}
            isLoading={tools.isLoading}
            hint={tools.error ? UNREADABLE : undefined}
          />
        </div>

        <div
          css={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(340px, 1fr))",
            gap: theme.space(4),
            alignItems: "start",
          }}
        >
          <Card
            title="Discovered tools per MCP server"
            data-testid="dashboard-tools-card"
          >
            <ToolsPerServerChart
              servers={servers.error ? [] : (servers.data ?? [])}
              hasError={servers.error !== undefined}
              isLoading={servers.isLoading}
            />
          </Card>

          <Card title="Recent agent conversations" data-testid="dashboard-recent-card">
            <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
              {agents.error ? (
                <Text
                  data-testid="recent-agents-unavailable"
                  css={{ color: theme.color.textMuted }}
                >
                  Recent activity is unavailable while the agent list cannot be read.
                </Text>
              ) : recent.length === 0 ? (
                agents.isLoading ? null : (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    data-testid="recent-agents-empty"
                    description="No agents yet."
                  />
                )
              ) : (
                <>
                  <ReadinessMeter ready={readyCount} total={agentRows.length} />
                  <ul
                    data-testid="recent-agents"
                    css={{ listStyle: "none", margin: 0, padding: 0 }}
                  >
                    {recent.map((row) => (
                      <RecentAgent
                        key={`${row.namespace}/${row.id}`}
                        row={row}
                        autoTitle={derivedTitles[row.id]}
                      />
                    ))}
                  </ul>
                </>
              )}
            </Space>
          </Card>
        </div>
      </Space>
    </PageFrame>
  );
}

const UNREADABLE = "Could not be read";

/** The agent tile's sub-line: readiness when known, why not when it is not. */
function agentsHint(
  hasError: boolean,
  data: AgentInstance[] | undefined,
  readyCount: number,
): string | undefined {
  if (hasError) return UNREADABLE;
  if (!data) return undefined;
  return `${readyCount} ready`;
}

/** One row of the recent-activity list: which agent it is, whether it came up, and when. */
function RecentAgent({ row, autoTitle }: { row: AgentInstance; autoTitle?: string }) {
  const theme = useTheme();

  return (
    <li
      data-testid="recent-agent"
      css={{
        display: "flex",
        alignItems: "baseline",
        justifyContent: "space-between",
        gap: theme.space(3),
        padding: `${theme.space(2)} 0`,
        borderBottom: `1px solid ${theme.color.border}`,
        "&:last-of-type": { borderBottom: "none" },
      }}
    >
      <div css={{ minWidth: 0 }}>
        <Link
          to={buildPath(paths.agentChat, { namespace: row.namespace, id: row.id })}
          css={{ fontWeight: 500 }}
        >
          {/* The name the reader gave it, the title derived from its first message, or
              "Untitled" and the short id — the same three answers in the same order as
              the rail and the conversations table. This used to be the bare short id,
              on the reasoning that a conversation had no name to show. It does now. */}
          {conversationTitle(row, autoTitle)}
        </Link>
        <Text css={{ display: "block", color: theme.color.textMuted, fontSize: 12 }}>
          {row.namespace} · {row.agentTemplate ?? "template not reported"}
        </Text>
      </div>
      <div css={{ flexShrink: 0, textAlign: "right" }}>
        {/* The word carries the state and the colour only reinforces it: red and
            green are hard to tell apart for a good share of readers. */}
        {row.state === "ready" ? (
          <Tag color="success">Ready</Tag>
        ) : (
          <Tag color="warning">Not ready</Tag>
        )}
        <Text css={{ display: "block", color: theme.color.textMuted, fontSize: 12 }}>
          {relativeAge(row.createdAt)}
        </Text>
      </div>
    </li>
  );
}

/** "3 days ago", or an em dash when the cluster gave us no timestamp. */
function relativeAge(timestamp: string | undefined): string {
  if (!timestamp) return "—";
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) return "—";
  return `${formatDistanceToNow(parsed)} ago`;
}

/** "agents and tools", "agents, MCP servers and tools" — a readable list. */
function listNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}
