import { useTheme } from "@emotion/react";
import { useMemo, useState } from "react";
import { Alert, Button, Descriptions, Space, Table, Tag, Typography } from "antd";
import { ArrowLeft, CalendarClock, ChevronsDownUp, ChevronsUpDown, ExternalLink, Globe, Pause, Pencil, Play, Plus } from "lucide-react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import { invoke } from "@/api/operations";
import { useApiResource } from "@/api/hooks/useApiResource";
import { useInvalidateScheduledRuns } from "@/api/hooks/useInvalidateScheduledRuns";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { SearchInput } from "@/components/table/SearchInput";
import { PageControls, usePageStack } from "@/components/table/PageControls";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { clickableRow, useExpandedRows } from "@/components/table/rowClick";
import { scheduleDescription } from "@/components/scheduled-runs/scheduleTiming";
import { linkStyles } from "@/components/common/linkStyles";
import { buildPath, paths } from "@/router/routes";
import { useUrlStateWriter } from "@/router/useUrlState";
import { ScheduledRunExecutionState, type ScheduledRun, type ScheduledRunExecution } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";

function time(value: Timestamp | undefined) {
  return value ? timestampDate(value).toLocaleString() : "—";
}

export function ScheduledRunsPage() {
  const theme = useTheme();
  const invalidate = useInvalidateScheduledRuns();
  const navigate = useNavigate();
  const page = usePageStack("schedules", "pages");
  const runs = useApiResource(["scheduledRuns.list", page.current],
    () => invoke("scheduledRuns.list", { page: { limit: 25, pageToken: page.current } }), { refreshInterval: 10000 });

  return <PageFrame title="Schedules" description="Run an agent automatically. Each execution starts a new conversation."
    actions={<Space size={8}><RefreshButton onRefresh={runs.refresh} what="Schedules" loading={runs.isValidating} />
      <Link to={paths.scheduledRunNew}>
        <Button type="primary" icon={<Plus size={14} />} data-testid="schedules-new">New Schedule</Button>
      </Link></Space>}>
    <Space orientation="vertical" css={{ display: "flex", ...linkStyles(theme) }} size="middle">
      {runs.error && <Alert type="error" showIcon title="Could not load schedules" description={runs.error.message} />}
      <Table<ScheduledRun> rowKey="id" loading={runs.isLoading} pagination={false} scroll={{ x: 800 }}
        dataSource={runs.data?.scheduledRuns ?? []} locale={{ emptyText: runs.error ? "Schedules unavailable" : "No schedules yet" }} columns={[
          { title: "Name", key: "name", render: (_, row) => <Link to={buildPath(paths.scheduledRun, { id: row.id })}>{row.config?.name || row.id}</Link> },
          { title: "Agent", key: "agent", render: (_, row) => `${row.agentTemplate?.name ?? "—"} on ${row.harness?.name ?? "—"}` },
          { title: "Schedule", key: "schedule", render: (_, row) => row.config ? scheduleDescription(row.config.schedule) : "—" },
          { title: "Time zone", key: "zone", render: (_, row) => row.config?.timeZone || "UTC" },
          { title: "Status", key: "status", render: (_, row) => scheduleStatusTag(row) },
          { title: "Next execution (local)", key: "next", render: (_, row) => time(row.nextExecutionTime) },
          // Last and untitled, as on every other list: a row's actions belong at the
          // end of it, past the data they act on.
          { title: "", key: "actions", width: 76, render: (_, row) => {
            const name = row.config?.name || row.id;
            return <Space size={0}>
              <Link to={buildPath(paths.scheduledRunEdit, { id: row.id })}>
                <Button type="text" size="small" icon={<Pencil size={14} />}
                  data-testid={`edit-${name}`} aria-label={`Edit schedule ${name}`} />
              </Link>
              <DeleteResourceButton kind="schedule" name={name}
                description="Stops future executions. Accepted executions continue; history and conversations are retained."
                onDelete={async () => { await invoke("scheduledRuns.delete", { scheduledRunId: row.id }); }}
                onDeleted={invalidate} />
            </Space>;
          } },
        ]}
        // The Name cell stays a link, so the row is reachable by keyboard; the guard
        // sees that link — and the row's edit and delete — and leaves them alone.
        onRow={(row) => clickableRow(() => void navigate(buildPath(paths.scheduledRun, { id: row.id })))} />
      <PageControls testId="schedules-pages" page={page} hasNext={Boolean(runs.data?.page?.nextPageToken)}
        onNext={() => page.next(runs.data?.page?.nextPageToken ?? "")} onBack={page.back} isLoading={runs.isLoading} />
    </Space>
  </PageFrame>;
}

export function ScheduledRunPage() {
  const { id } = useParams();
  // Remount action and pagination state when navigating between schedules.
  return id ? <ScheduledRunDetails key={id} id={id} /> : <Alert type="error" title="Missing schedule ID" />;
}

function ScheduledRunDetails({ id }: { id: string }) {
  const theme = useTheme();
  const invalidate = useInvalidateScheduledRuns();
  const navigate = useNavigate();
  // Which action is in flight, so only that button spins.
  const [busy, setBusy] = useState<"pause" | "trigger">();
  const [actionError, setActionError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [triggerRequestId, setTriggerRequestId] = useState<string>();
  const page = usePageStack(id, "pages");
  /* Search in the address, like a list's filters: it describes which rows are being
     looked at, so a reload and a shared link should agree. Typing resets the page,
     because a cursor from the unfiltered list means nothing once narrowed. */
  const [searchParams] = useSearchParams();
  const write = useUrlStateWriter();
  const query = searchParams.get("q") ?? "";
  const setQuery = (next: string) => write({ q: next || null, pages: null });
  const run = useApiResource(["scheduledRuns.get", id], () => invoke("scheduledRuns.get", { scheduledRunId: id }), { refreshInterval: 10000 });
  const history = useApiResource(["scheduledRuns.executions", id, page.current],
    () => invoke("scheduledRuns.executions", { scheduledRunId: id, page: { limit: 25, pageToken: page.current } }), { refreshInterval: 5000 });
  const schedule = run.data?.scheduledRun;
  const config = schedule?.config;
  const disabled = !!busy || !!run.error || !!schedule?.deletedAt;

  const executions = useMemo(() => history.data?.executions ?? [], [history.data]);
  const { rows, revealed } = useMemo(() => searchExecutions(executions, query), [executions, query]);
  /* Seeded with what the search matched out of sight, so the reader can see why a row
     came back — and can still close it. */
  const expanded = useExpandedRows({ keys: revealed, when: query });

  const visibleKeys = rows.map((row) => row.id);

  async function refresh() {
    await Promise.all([run.refresh(), history.refresh()]);
  }

  async function act(operation: "pause" | "trigger") {
    if (!schedule || !config) return;
    setBusy(operation);
    setActionError(undefined);
    setNotice(undefined);
    try {
      if (operation === "pause") {
        await invoke("scheduledRuns.update", { scheduledRunId: id, etag: schedule.etag, config: { ...config, paused: !config.paused } });
      } else {
        // Retained across a failed retry so it cannot queue a second execution.
        const requestId = triggerRequestId ?? crypto.randomUUID();
        setTriggerRequestId(requestId);
        await invoke("scheduledRuns.trigger", { scheduledRunId: id, requestId });
        setTriggerRequestId(undefined);
        page.reset();
        setNotice("Execution queued. A new conversation will appear when the agent starts.");
      }
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      // A failed refresh must not turn an accepted trigger into a retry.
      try { await refresh(); } catch (cause) {
        setActionError((previous) => previous ?? `Could not refresh: ${cause instanceof Error ? cause.message : String(cause)}`);
      }
      setBusy(undefined);
    }
  }

  /* Top right, not under the description list: there Run sat below the fold on any
     schedule with a few firings. Ordered leaving-to-doing, so the filled Run — the
     action this page exists for — is rightmost and Delete is not beside it at all. */
  const controls = <div css={{ display: "flex", flexWrap: "wrap", gap: theme.space(2), justifyContent: "flex-end" }}>
    <Link to={paths.scheduledRuns}><Button icon={<ArrowLeft size={14} />}>Back</Button></Link>
    <RefreshButton onRefresh={refresh} what="Schedule" loading={run.isValidating || history.isValidating} />
    <Button icon={<Pencil size={14} />} disabled={disabled}
      onClick={() => void navigate(buildPath(paths.scheduledRunEdit, { id }))}>Edit</Button>
    <Button icon={config?.paused ? <Play size={14} /> : <Pause size={14} />} disabled={disabled || !config} loading={busy === "pause"}
      onClick={() => void act("pause")}>{config?.paused ? "Resume" : "Pause"}</Button>
    <Button type="primary" icon={<Play size={14} />} disabled={disabled || !config} loading={busy === "trigger"}
      onClick={() => void act("trigger")}>Run</Button>
  </div>;

  /* Its own header: `PageFrame` holds actions at a fixed width, which squeezed this
     title to a letter per line once there were six of them. */
  return <PageFrame>
    <div css={{ display: "flex", flexWrap: "wrap", alignItems: "flex-start",
      justifyContent: "space-between", gap: theme.space(4), marginBottom: theme.space(6) }}>
      <div css={{ flex: "1 1 320px", minWidth: 0 }}>
        <Typography.Title level={2} css={{ margin: 0 }} data-testid="page-title">{config?.name || "Schedule"}</Typography.Title>
        {/* What the schedule is, in one line under its name: whether it is running,
            how often, and in whose clock. The rest is in the list below. */}
        {schedule && config && <Space size={8} wrap data-testid="schedule-meta" css={{ marginTop: theme.space(2) }}>
          {scheduleStatusTag(schedule)}
          <Tag icon={<CalendarClock size={12} />}>{scheduleDescription(config.schedule)}</Tag>
          <Tag icon={<Globe size={12} />}>{config.timeZone || "UTC"}</Tag>
        </Space>}
      </div>
      {controls}
    </div>
    <Space orientation="vertical" size="middle" css={{ display: "flex", ...linkStyles(theme) }}>
      {run.error && <Alert type="error" showIcon title="Could not load schedule" description={run.error.message} />}
      {actionError && <Alert type="error" showIcon title="Schedule action failed" description={actionError} />}
      {notice && <Alert type="success" showIcon title={notice} />}
      {schedule?.deletedAt && <Alert type="info" showIcon title="This schedule was deleted. Its execution history is retained." />}
      {schedule && config && <Descriptions bordered column={{ xs: 1, sm: 2 }} items={[
        { key: "agent", label: "Agent", children: schedule.agentTemplate && schedule.harness
          ? <Link to={buildPath(paths.agent, { namespace: schedule.agentTemplate.namespace, agentTemplate: schedule.agentTemplate.name, harness: schedule.harness.name })}>
            {schedule.agentTemplate.namespace}/{schedule.agentTemplate.name} on {schedule.harness.name}</Link> : "—" },
        { key: "next", label: "Next execution (local)", children: time(schedule.nextExecutionTime) },
        { key: "created", label: "Created", children: time(schedule.createdAt) },
        { key: "timeout", label: "Execution timeout", children: config.executionTimeout ? `${Number(config.executionTimeout.seconds) + config.executionTimeout.nanos / 1e9} seconds` : "15 minutes" },
        { key: "prompt", label: "Prompt", span: 2, children: <Typography.Paragraph css={{ whiteSpace: "pre-wrap", margin: 0 }}>{config.prompt}</Typography.Paragraph> },
      ]} />}
      <Typography.Title level={3}>Execution history</Typography.Title>
      <Typography.Text css={{ color: theme.color.textMuted }}>Times are shown in your local time zone.</Typography.Text>
      <div css={{ display: "flex", flexWrap: "wrap", gap: theme.space(2), alignItems: "center" }}>
        <SearchInput value={query} onChange={setQuery} testId="history-search"
          label="Search execution history" placeholder="Search the history…" />
        <Button data-testid="history-expand-all" disabled={visibleKeys.length === 0}
          icon={expanded.allExpanded(visibleKeys) ? <ChevronsDownUp size={14} /> : <ChevronsUpDown size={14} />}
          onClick={() => expanded.allExpanded(visibleKeys) ? expanded.collapseAll() : expanded.expandAll(visibleKeys)}>
          {expanded.allExpanded(visibleKeys) ? "Collapse all" : "Expand all"}
        </Button>
      </div>
      {history.error && <Alert type="error" showIcon title="Could not load execution history" description={history.error.message} />}
      <Table<ScheduledRunExecution> rowKey="id" loading={history.isLoading} pagination={false} scroll={{ x: 800 }}
        dataSource={rows} locale={{ emptyText: history.error ? "History unavailable"
          // Three different facts. A search that matched nothing is not an empty history,
          // and neither is a failed read. Only this one names the page, because only here
          // does the scope explain the answer.
          : executions.length === 0 ? "No executions yet" : "Nothing on this page of the history matches that search." }} columns={[
          { title: "Created", key: "created", render: (_, row) => time(row.createdAt) },
          { title: "Trigger", key: "trigger", render: (_, row) => triggerLabel(row) },
          { title: "State", key: "state", render: (_, row) => executionStateTag(row.state) },
          { title: "Completed", key: "completed", render: (_, row) => time(row.completedAt) },
          { title: "Failure reason", key: "failureReason", render: (_, row) => row.failureReason || "—" },
          { title: "Conversation", key: "conversation", render: (_, row) => row.agentInstanceId
            ? <Link to={buildPath(paths.agentChat, { id: row.agentInstanceId })} css={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
              Open conversation<ExternalLink size={14} /></Link> : "Not started" },
        ]} expandable={{ expandedRowRender: (row) => <Descriptions column={1} items={[
          { key: "prompt", label: "Prompt", children: <span css={{ whiteSpace: "pre-wrap" }}>{row.prompt}</span> },
          { key: "deadline", label: "Deadline", children: time(row.deadline) },
          { key: "task", label: "Original task", children: row.taskId || "Not assigned" },
        ]} />,
          // Controlled rather than `expandRowByClick`, so `clickableRow` can let the
          // row's conversation link navigate instead of unfolding the row.
          expandedRowKeys: expanded.keys, onExpand: (open, row) => expanded.set(row.id, open) }}
        onRow={(row) => clickableRow(() => expanded.toggle(row.id))} />
      <PageControls testId="schedule-history-pages" page={page} hasNext={Boolean(history.data?.page?.nextPageToken)}
        onNext={() => page.next(history.data?.page?.nextPageToken ?? "")} onBack={page.back} isLoading={history.isLoading} />
      {/* A sibling of Execution history, not a panel inside a red box. `DeleteResourceButton`
          is already `danger`, so the warning sits on the control that does the destroying;
          a tinted ground on top of that made the quietest thing on the page the loudest. */}
      <div data-testid="schedule-danger" css={{ display: "flex", flexDirection: "column",
        alignItems: "flex-start", gap: theme.space(3), marginTop: theme.space(4) }}>
        <Typography.Title level={3} css={{ margin: 0 }}>Danger zone</Typography.Title>
        {/* `textMuted`, not antd's `type="secondary"`: this theme never overrides that
            token and it measures 2.85:1 on the light page ground. */}
        <Typography.Text css={{ color: theme.color.textMuted }}>
          Deleting this schedule stops future executions. Accepted executions continue; history and conversations are retained.
        </Typography.Text>
        <DeleteResourceButton kind="schedule" name={config?.name || id} label="Delete schedule" confirmation="modal" outlined disabled={disabled}
          description="Stops future executions. Accepted executions continue; history and conversations are retained."
          onDelete={async () => { await invoke("scheduledRuns.delete", { scheduledRunId: id }); }}
          onDeleted={async () => { await invalidate(); await navigate(paths.scheduledRuns); }} />
      </div>
    </Space>
  </PageFrame>;
}

function scheduleStatusTag(schedule: ScheduledRun) {
  if (schedule.deletedAt) return <Tag>Deleted</Tag>;
  return schedule.config?.paused ? <Tag color="warning">Paused</Tag> : <Tag color="success">Active</Tag>;
}

function triggerLabel(execution: ScheduledRunExecution) {
  switch (execution.trigger.case) {
    case "scheduledTime": return `Scheduled: ${time(execution.trigger.value)}`;
    case "manualRequestId": return "Manual";
    default: return "Unknown";
  }
}

/* Split from the tag so search can match what the reader sees. Matching the enum name
   would leave "Timed out" finding nothing while the screen says exactly that. */
function executionStateLabel(state: ScheduledRunExecutionState) {
  switch (state) {
    case ScheduledRunExecutionState.PENDING: return "Pending";
    case ScheduledRunExecutionState.RUNNING: return "Running";
    case ScheduledRunExecutionState.SUCCEEDED: return "Succeeded";
    case ScheduledRunExecutionState.FAILED: return "Failed";
    case ScheduledRunExecutionState.TIMED_OUT: return "Timed out";
    default: return "Unknown";
  }
}

const STATE_COLOUR: Partial<Record<ScheduledRunExecutionState, string>> = {
  [ScheduledRunExecutionState.RUNNING]: "processing",
  [ScheduledRunExecutionState.SUCCEEDED]: "success",
  [ScheduledRunExecutionState.FAILED]: "error",
  [ScheduledRunExecutionState.TIMED_OUT]: "warning",
};

/* The label carries the state on its own; colour is a second channel, not the only one. */
function executionStateTag(state: ScheduledRunExecutionState) {
  return <Tag color={STATE_COLOUR[state]}>{executionStateLabel(state)}</Tag>;
}

/** What a row shows in its columns, and what it shows only once unfolded. */
function executionText(row: ScheduledRunExecution) {
  return {
    /* Every value as it is rendered, not as it is stored: a timestamp through
       `toLocaleString` and a state through its label, so what is on screen is what
       matches. */
    columns: [time(row.createdAt), triggerLabel(row), executionStateLabel(row.state),
      time(row.completedAt), row.failureReason || "—",
      row.agentInstanceId ? "Open conversation" : "Not started"].join(" ").toLowerCase(),
    panel: [row.prompt, time(row.deadline), row.taskId || "Not assigned"].join(" ").toLowerCase(),
  };
}

/**
 * The executions matching `query`, and which of them matched only out of sight.
 *
 * Page-scoped by necessity — the RPC takes a page and no filter — so this narrows what
 * is already loaded. `revealed` is what the caller unfolds: a row returned for a word
 * only its panel contains is a match the reader would otherwise have to guess at.
 */
function searchExecutions(executions: ScheduledRunExecution[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return { rows: executions, revealed: [] as string[] };

  const rows: ScheduledRunExecution[] = [];
  const revealed: string[] = [];
  for (const row of executions) {
    const text = executionText(row);
    const inColumns = text.columns.includes(needle);
    if (!inColumns && !text.panel.includes(needle)) continue;
    rows.push(row);
    if (!inColumns) revealed.push(row.id);
  }
  return { rows, revealed };
}
