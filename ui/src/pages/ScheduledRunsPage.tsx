import { useTheme } from "@emotion/react";
import { useState } from "react";
import { Alert, Button, Descriptions, Space, Table, Tag, Typography } from "antd";
import { Pause, Pencil, Play, Plus } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import { invoke } from "@/api/operations";
import { useApiResource } from "@/api/hooks/useApiResource";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { PageControls, usePageStack } from "@/components/table/PageControls";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { scheduleDescription } from "@/components/scheduled-runs/scheduleTiming";
import { ScheduledRunForm } from "@/components/scheduled-runs/ScheduledRunForm";
import { buildPath, paths } from "@/router/routes";
import { ScheduledRunExecutionState, type ScheduledRun, type ScheduledRunExecution } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";

function time(value: Timestamp | undefined) {
  return value ? timestampDate(value).toLocaleString() : "—";
}

export function ScheduledRunsPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const page = usePageStack("schedules");
  const runs = useApiResource(["scheduledRuns.list", page.current],
    () => invoke("scheduledRuns.list", { page: { limit: 25, pageToken: page.current } }), { refreshInterval: 10000 });

  return <PageFrame title="Schedules" description="Run an agent automatically. Each execution starts a new conversation."
    actions={<Space><RefreshButton onRefresh={runs.refresh} what="Schedules" loading={runs.isValidating} />
      <Button type="primary" icon={<Plus size={14} />} onClick={() => setCreating(true)}>New Schedule</Button></Space>}>
    <Space orientation="vertical" css={{ display: "flex", a: { color: theme.color.primaryText } }} size="middle">
      {runs.error && <Alert type="error" showIcon title="Could not load schedules" description={runs.error.message} />}
      <Table<ScheduledRun> rowKey="id" loading={runs.isLoading} pagination={false} scroll={{ x: 800 }}
        dataSource={runs.data?.scheduledRuns ?? []} locale={{ emptyText: runs.error ? "Schedules unavailable" : "No schedules yet" }} columns={[
          { title: "Name", key: "name", render: (_, row) => <Link to={buildPath(paths.scheduledRun, { id: row.id })}>{row.config?.name || row.id}</Link> },
          { title: "Agent", key: "agent", render: (_, row) => `${row.agentTemplate?.name ?? "—"} on ${row.harness?.name ?? "—"}` },
          { title: "Schedule", key: "schedule", render: (_, row) => row.config ? scheduleDescription(row.config.schedule) : "—" },
          { title: "Time zone", key: "zone", render: (_, row) => row.config?.timeZone || "UTC" },
          { title: "Status", key: "status", render: (_, row) => scheduleStatusTag(row) },
          { title: "Next execution (local)", key: "next", render: (_, row) => time(row.nextExecutionTime) },
        ]} />
      <PageControls testId="schedules-pages" page={page} hasNext={Boolean(runs.data?.page?.nextPageToken)}
        onNext={() => page.next(runs.data?.page?.nextPageToken ?? "")} onBack={page.back} isLoading={runs.isLoading} />
    </Space>
    {creating && <ScheduledRunForm onClose={() => setCreating(false)} onSaved={(schedule) => {
      setCreating(false);
      void navigate(buildPath(paths.scheduledRun, { id: schedule.id }));
    }} />}
  </PageFrame>;
}

export function ScheduledRunPage() {
  const { id } = useParams();
  // Remount action and pagination state when navigating between schedules.
  return id ? <ScheduledRunDetails key={id} id={id} /> : <Alert type="error" title="Missing schedule ID" />;
}

function ScheduledRunDetails({ id }: { id: string }) {
  const theme = useTheme();
  const [editing, setEditing] = useState<ScheduledRun>();
  // Which action is in flight, so only that button spins.
  const [busy, setBusy] = useState<"pause" | "trigger">();
  const [actionError, setActionError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [triggerRequestId, setTriggerRequestId] = useState<string>();
  const page = usePageStack(id);
  const run = useApiResource(["scheduledRuns.get", id], () => invoke("scheduledRuns.get", { scheduledRunId: id }), { refreshInterval: 10000 });
  const history = useApiResource(["scheduledRuns.executions", id, page.current],
    () => invoke("scheduledRuns.executions", { scheduledRunId: id, page: { limit: 25, pageToken: page.current } }), { refreshInterval: 5000 });
  const schedule = run.data?.scheduledRun;
  const config = schedule?.config;
  const disabled = !!busy || !!run.error || !!schedule?.deletedAt;

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

  return <PageFrame title={config?.name || "Schedule"} actions={<Space wrap>
    <Link to={paths.scheduledRuns}><Button>Back to schedules</Button></Link>
    <RefreshButton onRefresh={refresh} what="Schedule" loading={run.isValidating || history.isValidating} />
  </Space>}>
    <Space orientation="vertical" size="middle" css={{ display: "flex", a: { color: theme.color.primaryText } }}>
      {run.error && <Alert type="error" showIcon title="Could not load schedule" description={run.error.message} />}
      {actionError && <Alert type="error" showIcon title="Schedule action failed" description={actionError} />}
      {notice && <Alert type="success" showIcon title={notice} />}
      {schedule?.deletedAt && <Alert type="info" showIcon title="This schedule was deleted. Its execution history is retained." />}
      {schedule && config && <>
        <Descriptions bordered column={{ xs: 1, sm: 2 }} items={[
          { key: "agent", label: "Agent", children: schedule.agentTemplate && schedule.harness
            ? <Link to={buildPath(paths.agent, { namespace: schedule.agentTemplate.namespace, agentTemplate: schedule.agentTemplate.name, harness: schedule.harness.name })}>
              {schedule.agentTemplate.namespace}/{schedule.agentTemplate.name} on {schedule.harness.name}</Link> : "—" },
          { key: "status", label: "Status", children: scheduleStatusTag(schedule) },
          { key: "schedule", label: "Schedule", children: scheduleDescription(config.schedule) },
          { key: "zone", label: "Time zone", children: config.timeZone || "UTC" },
          { key: "next", label: "Next execution (local)", children: time(schedule.nextExecutionTime) },
          { key: "timeout", label: "Execution timeout", children: config.executionTimeout ? `${Number(config.executionTimeout.seconds) + config.executionTimeout.nanos / 1e9} seconds` : "15 minutes" },
          { key: "prompt", label: "Prompt", span: 2, children: <Typography.Paragraph css={{ whiteSpace: "pre-wrap", margin: 0 }}>{config.prompt}</Typography.Paragraph> },
        ]} />
        <Space wrap>
          <Button icon={<Play size={14} />} disabled={disabled} loading={busy === "trigger"} onClick={() => void act("trigger")}>Run</Button>
          <Button icon={config.paused ? <Play size={14} /> : <Pause size={14} />} disabled={disabled} loading={busy === "pause"}
            onClick={() => void act("pause")}>{config.paused ? "Resume" : "Pause"}</Button>
          <Button icon={<Pencil size={14} />} disabled={disabled} onClick={() => setEditing(schedule)}>Edit</Button>
          <DeleteResourceButton kind="schedule" name={config.name || id} label="Delete" confirmation="modal" outlined disabled={disabled}
            description="Stops future executions. Accepted executions continue; history and conversations are retained."
            onDelete={async () => { await invoke("scheduledRuns.delete", { scheduledRunId: id }); }} onDeleted={refresh} />
        </Space>
      </>}
      <Typography.Title level={3}>Execution history</Typography.Title>
      <Typography.Text type="secondary">Times are shown in your local time zone. Conversation links may refer to conversations that have since been deleted.</Typography.Text>
      {history.error && <Alert type="error" showIcon title="Could not load execution history" description={history.error.message} />}
      <Table<ScheduledRunExecution> rowKey="id" loading={history.isLoading} pagination={false} scroll={{ x: 800 }}
        dataSource={history.data?.executions ?? []} locale={{ emptyText: history.error ? "History unavailable" : "No executions yet" }} columns={[
          { title: "Created", key: "created", render: (_, row) => time(row.createdAt) },
          { title: "Trigger", key: "trigger", render: (_, row) => triggerLabel(row) },
          { title: "State", key: "state", render: (_, row) => executionStateTag(row.state) },
          { title: "Completed", key: "completed", render: (_, row) => time(row.completedAt) },
          { title: "Failure reason", key: "failureReason", render: (_, row) => row.failureReason || "—" },
          { title: "Conversation", key: "conversation", render: (_, row) => row.agentInstanceId
            ? <Link to={buildPath(paths.agentChat, { id: row.agentInstanceId })}>Open conversation</Link> : "Not started" },
        ]} expandable={{ expandedRowRender: (row) => <Descriptions column={1} items={[
          { key: "prompt", label: "Prompt", children: <span css={{ whiteSpace: "pre-wrap" }}>{row.prompt}</span> },
          { key: "deadline", label: "Deadline", children: time(row.deadline) },
          { key: "task", label: "Original task", children: row.taskId || "Not assigned" },
        ]} /> }} />
      <PageControls testId="schedule-history-pages" page={page} hasNext={Boolean(history.data?.page?.nextPageToken)}
        onNext={() => page.next(history.data?.page?.nextPageToken ?? "")} onBack={page.back} isLoading={history.isLoading} />
    </Space>
    {editing && <ScheduledRunForm schedule={editing} onClose={() => setEditing(undefined)} onSaved={() => {
      setEditing(undefined);
      void refresh().catch((cause: unknown) => setActionError(`Saved, but could not refresh: ${cause instanceof Error ? cause.message : String(cause)}`));
    }} />}
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

/* The label carries the state on its own; colour is a second channel, not the only one. */
function executionStateTag(state: ScheduledRunExecutionState) {
  switch (state) {
    case ScheduledRunExecutionState.PENDING: return <Tag>Pending</Tag>;
    case ScheduledRunExecutionState.RUNNING: return <Tag color="processing">Running</Tag>;
    case ScheduledRunExecutionState.SUCCEEDED: return <Tag color="success">Succeeded</Tag>;
    case ScheduledRunExecutionState.FAILED: return <Tag color="error">Failed</Tag>;
    case ScheduledRunExecutionState.TIMED_OUT: return <Tag color="warning">Timed out</Tag>;
    default: return <Tag>Unknown</Tag>;
  }
}
