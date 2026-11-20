import { Alert, Button, Card, Skeleton, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Plus } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { linkInk } from "@/components/common/linkStyles";
import { rowClickHandler } from "@/components/table/rowClick";
import { scheduleDescription } from "@/components/scheduled-runs/scheduleTiming";
import { buildPath, paths } from "@/router/routes";
import { rowStateStyles } from "./controlStyles";
import type { ScheduledAgentRef } from "./scheduleTargets";
import { useAgentSchedules } from "./useAgentSchedules";

const { Text } = Typography;

/**
 * The schedules that run this agent.
 *
 * Every occurrence starts a fresh conversation, so an agent with a schedule is doing
 * work nobody in its conversation list asked for by hand.
 *
 * Shown in its own tab on the agent's page; the tab label carries the count.
 */
export function AgentSchedules({ agent }: { agent: ScheduledAgentRef }) {
  const theme = useTheme();
  const navigate = useNavigate();

  const { schedules, rows = [] } = useAgentSchedules(agent);

  return (
    <Card size="small" data-testid="agent-schedules">
      {schedules.error ? (
        // A failed read is not an empty list: the create action and the "none yet"
        // wording are both withheld, because neither is known to be true.
        <Alert
          type="error"
          showIcon
          title="Could not load this agent's schedules"
          description={schedules.error.message}
          data-testid="agent-schedules-error"
          action={
            <Button size="small" onClick={() => void schedules.refresh()}>
              Try again
            </Button>
          }
        />
      ) : schedules.isLoading ? (
        <Skeleton
          active
          title={false}
          paragraph={{ rows: 2 }}
          data-testid="agent-schedules-loading"
        />
      ) : rows.length > 0 ? (
        <ul
          data-testid="agent-schedules-list"
          css={{ listStyle: "none", margin: 0, padding: 0 }}
        >
          {rows.map((schedule) => (
            <li
              key={schedule.id}
              data-testid={`agent-schedule-${schedule.id}`}
              onClick={rowClickHandler(() =>
                void navigate(buildPath(paths.scheduledRun, { id: schedule.id })),
              )}
              css={{
                display: "flex",
                // Wraps rather than scrolls: on a narrow window the cadence and the
                // status drop under the name instead of leaving the card.
                flexWrap: "wrap",
                alignItems: "center",
                gap: theme.space(3),
                paddingBlock: theme.space(2),
                paddingInline: theme.space(2),
                marginInline: `-${theme.space(2)}`,
                borderRadius: theme.radius.sm,
                cursor: "pointer",
                // The table rows' hover and press: `clickable-table-row` reaches no `li`.
                ...rowStateStyles(theme),
                "&:not(:last-of-type)": {
                  borderBottom: `1px solid ${theme.color.border}`,
                },
              }}
            >
              <Link
                to={buildPath(paths.scheduledRun, { id: schedule.id })}
                data-testid={`agent-schedule-link-${schedule.id}`}
                css={linkInk(theme)}
              >
                {schedule.config?.name || schedule.id}
              </Link>
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                {schedule.config ? scheduleDescription(schedule.config.schedule) : "—"}
              </Text>
              {schedule.config?.paused ? (
                <Tag color="warning">Paused</Tag>
              ) : (
                <Tag color="success">Active</Tag>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <div
          data-testid="agent-schedules-empty"
          css={{
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: theme.space(3),
          }}
        >
          <Text css={{ color: theme.color.textMuted }}>
            Nothing runs this agent on a schedule yet.
          </Text>
          {/* Filled, not outlined: a default button's pressed ink comes from
              `colorPrimary`, a fill colour, and measured 1.7:1 on the dark theme. A
              primary button changes its fill and keeps `textOnPrimary` in every state. */}
          <Link to={paths.scheduledRunNew} data-testid="agent-schedules-create">
            <Button type="primary" size="small" icon={<Plus size={14} />}>
              Create a schedule
            </Button>
          </Link>
        </div>
      )}
    </Card>
  );
}
