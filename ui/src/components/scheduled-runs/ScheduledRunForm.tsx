import { useMemo, useState, type KeyboardEvent } from "react";
import { Alert, AutoComplete, Checkbox, Form, Input, InputNumber, Select, Switch } from "antd";
import { useTheme } from "@emotion/react";
import { fromJson } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { newConversationBlockedReason, useAgentsAcrossNamespaces, useNamespaces } from "@/api";
import { isPermissionDenied } from "@/api/ApiError";
import { invoke } from "@/api/operations";
import { randomId } from "@/api/randomId";
import { useInvalidateScheduledRuns } from "@/api/hooks/useInvalidateScheduledRuns";
import { SubmitError } from "@/components/common/SubmitError";
import { FormActions } from "@/components/form/FormActions";
import { FieldRow } from "@/components/form/FieldRow";
import { FormField } from "@/components/form/FormField";
import { toastSaved } from "@/components/form/saveToast";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import type { ScheduledRun } from "@/generated/kagent/api/v1/scheduled_runs_pb";
import { draftFromSchedule, scheduleIssues, TIMEOUT_MAX_SECONDS, type ScheduleDraft } from "./scheduleDraft";
import { minuteIntervals, scheduleCron, scheduleDescription, weekdays, type ScheduleTiming } from "./scheduleTiming";

const timeZones = ["UTC", ...Intl.supportedValuesOf("timeZone")].map((value) => ({ value }));

const timingOf = (draft: ScheduleDraft): ScheduleTiming => ({
  ...draft,
  minute: draft.minute ?? 0,
  monthDay: draft.monthDay ?? 1,
});

/**
 * The fields of one schedule, shared by the create and edit pages.
 *
 * A page rather than a modal: the timing controls change shape as the frequency
 * changes, so the form grows past a dialog's height and the reader loses the
 * submit button behind a scroll — and an address that can be linked to and
 * reloaded is what makes an edit resumable.
 */
export function ScheduledRunForm({ schedule, cancelTo, onSaved }: {
  schedule?: ScheduledRun;
  /** Where Cancel goes. */
  cancelTo: string;
  onSaved: (schedule: ScheduledRun) => void;
}) {
  const theme = useTheme();
  const invalidateSchedules = useInvalidateScheduledRuns();
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  // Retain the key after a failed response: retrying must not create another schedule.
  const [requestId] = useState(() => randomId());
  const namespaces = useNamespaces();
  const namespaceDenied = isPermissionDenied(namespaces.error);
  const agentNamespaces = namespaceDenied ? null : namespaces.data?.map((row) => row.name);
  const definitions = useAgentsAcrossNamespaces(schedule ? undefined : agentNamespaces);
  const agents = definitions.data?.agents ?? [];
  const config = schedule?.config;
  const [initial] = useState(() => draftFromSchedule(schedule));
  const [draft, setDraft] = useState(initial);
  const initialTiming = timingOf(initial);
  const loadError = (namespaceDenied ? undefined : namespaces.error) ?? definitions.error;
  const issues = useMemo(() => scheduleIssues(draft, { needsAgent: !schedule }), [draft, schedule]);
  const validation = useSubmitValidation(issues);
  const { blocker, release } = useUnsavedChangesGuard(JSON.stringify(draft) !== JSON.stringify(initial));
  const error = validation.errorFor;
  const status = (field: string) => (error(field) ? "error" : undefined);
  const timing = timingOf(draft);

  const update = (patch: Partial<ScheduleDraft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    setFailure(undefined);
  };

  async function submit() {
    setFailure(undefined);
    if (!validation.attempt()) return;
    setSaving(true);
    try {
      const cron = scheduleCron(timing);
      const nextConfig = {
        ...config,
        name: draft.name.trim(),
        // Preserve existing expressions when only unrelated fields were edited.
        schedule: config && cron === scheduleCron(initialTiming) ? config.schedule : cron,
        timeZone: draft.timeZone.trim() || "UTC",
        prompt: draft.prompt,
        paused: !draft.enabled,
        executionTimeout: config?.executionTimeout && draft.timeoutSeconds === initial.timeoutSeconds
          ? config.executionTimeout : fromJson(DurationSchema, `${draft.timeoutSeconds}s`),
      };
      let result: ScheduledRun | undefined;
      if (schedule) {
        result = (await invoke("scheduledRuns.update", {
          scheduledRunId: schedule.id, etag: schedule.etag, config: nextConfig,
        })).scheduledRun;
      } else {
        const agent = agents.find((entry) => entry.ref === draft.agent);
        if (!agent) throw new Error("Choose an available agent.");
        result = (await invoke("scheduledRuns.create", {
          requestId,
          agent: { namespace: agent.namespace, name: agent.name },
          config: nextConfig,
        })).scheduledRun;
      }
      if (!result) throw new Error("The API returned no schedule.");
      // Refreshes any schedule surface still on screen; SWR does not fetch a key with
      // no mounted subscriber, so what the caller navigates to re-reads on mount.
      await invalidateSchedules().catch(() => {});
      toastSaved(schedule ? "saved" : "created", "schedule", result.config?.name || result.id);
      release();
      onSaved(result);
    } catch (cause) {
      setFailure(cause);
    } finally {
      setSaving(false);
    }
  }

  // Enter in a single-line field submits; text areas and lists keep their own Enter.
  // Number boxes too: their Enter commits a clamped value this render has not seen yet.
  function submitOnEnter(event: KeyboardEvent) {
    const target = event.target as HTMLInputElement;
    if (event.key !== "Enter" || event.nativeEvent.isComposing || saving || target.tagName !== "INPUT") return;
    if (["combobox", "spinbutton"].includes(target.getAttribute("role") ?? "") || ["checkbox", "radio"].includes(target.type)) return;
    event.preventDefault();
    void submit();
  }
  const showCadence = timing.frequency !== "custom" && timing.time && timing.days.length > 0
    && draft.minute !== null && draft.monthDay !== null;

  // A hint on the last timing field, not a paragraph after its 24px margin.
  const cadence = showCadence ? <span role="status" data-testid="schedule-cadence">
    {scheduleDescription(scheduleCron(timing))} ({draft.timeZone.trim() || "UTC"})
  </span> : undefined;

  return <div css={{ display: "flex", flexDirection: "column", gap: theme.space(4), maxWidth: 720 }}>
    <Form layout="vertical" component="div" disabled={saving} onKeyDown={submitOnEnter}>
      {!schedule && <>
        {loadError && <Alert type="error" showIcon title="Could not load agents" description={loadError.message} />}
        {definitions.data?.refused.map((entry) => <Alert key={entry.namespace} type="warning" showIcon
          title={`Could not read agents in ${entry.namespace}`} description={entry.reason} />)}
      </>}
      <FieldRow>
        <FormField htmlFor="schedule-name-input" label="Schedule name" required={false} {...validation.field("name")}
          help="Leave empty to show the schedule's ID.">
          <Input id="schedule-name-input" data-testid="schedule-name" placeholder="e.g. Morning health summary" maxLength={200} status={status("name")}
            value={draft.name} onChange={(event) => update({ name: event.target.value })} />
        </FormField>
        {!schedule && <FormField htmlFor="schedule-agent-input" label="Agent" required {...validation.field("agent")}
          help="The agent that runs on this schedule.">
          <Select id="schedule-agent-input" data-testid="schedule-agent" showSearch={{ optionFilterProp: "label" }}
            loading={namespaces.isLoading || definitions.isLoading} status={status("agent")}
            placeholder="Choose an agent" value={draft.agent || undefined}
            onChange={(agent: string) => update({ agent })}
            options={agents.map((agent) => {
              const blocked = newConversationBlockedReason(agent);
              return { value: agent.ref, disabled: !!blocked,
                label: `${agent.namespace}/${agent.name}${blocked ? ` — ${blocked}` : ""}` };
            })} />
        </FormField>}
      </FieldRow>
      <FieldRow>
        <FormField htmlFor="schedule-frequency-input" label="Repeat" required help="How often the agent runs.">
          <Select id="schedule-frequency-input" data-testid="schedule-frequency" value={draft.frequency} options={[
            { value: "minutes", label: "Every few minutes" },
            { value: "hourly", label: "Hourly" },
            { value: "daily", label: "Daily" },
            { value: "weekly", label: "Weekly" },
            { value: "monthly", label: "Monthly" },
            { value: "custom", label: "Custom (advanced)" },
          ]} onChange={(frequency) => update(frequency === "custom"
            ? { frequency, cron: scheduleCron(timing) } : { frequency })} />
        </FormField>
        <FormField htmlFor="schedule-timezone-input" label="Time zone" required={false} {...validation.field("timeZone")}
          help="Includes daylight-saving changes.">
          <AutoComplete id="schedule-timezone-input" data-testid="schedule-timezone" options={timeZones} placeholder="e.g. Europe/London"
            maxLength={253} status={status("timeZone")} value={draft.timeZone} onChange={(timeZone) => update({ timeZone })}
            filterOption={(input, option) => !!option?.value.toLowerCase().includes(input.toLowerCase())} />
        </FormField>
      </FieldRow>
      {draft.frequency === "weekly" && <FormField label="On days" required {...validation.field("days")}>
        <Checkbox.Group data-testid="schedule-days" value={draft.days} onChange={(days) => update({ days: days as number[] })}
          options={[1, 2, 3, 4, 5, 6, 0].map((value) => ({ value, label: weekdays[value] }))} />
      </FormField>}
      <FieldRow>
        {draft.frequency === "minutes" && <FormField htmlFor="schedule-interval-input" label="Every" required extra={cadence}>
          <Select id="schedule-interval-input" data-testid="schedule-interval" value={draft.interval}
            onChange={(interval) => update({ interval })}
            options={minuteIntervals.map((value) => ({ value, label: `${value} minute${value === 1 ? "" : "s"}` }))} />
        </FormField>}
        {draft.frequency === "hourly" && <FormField htmlFor="schedule-minute-input" label="Minute past the hour"
          required {...validation.field("minute")} extra={cadence}>
          <InputNumber id="schedule-minute-input" data-testid="schedule-minute" placeholder="Minute, e.g. 15" min={0} max={59} precision={0}
            status={status("minute")} value={draft.minute} onChange={(minute) => update({ minute })} />
        </FormField>}
        {draft.frequency === "monthly" && <FormField htmlFor="schedule-month-day-input" label="Day of the month"
          required {...validation.field("monthDay")} help="Months without this day are skipped.">
          <InputNumber id="schedule-month-day-input" data-testid="schedule-month-day" placeholder="Day, e.g. 1" min={1} max={31} precision={0}
            status={status("monthDay")} value={draft.monthDay} onChange={(monthDay) => update({ monthDay })} />
        </FormField>}
        {["daily", "weekly", "monthly"].includes(draft.frequency) && <FormField htmlFor="schedule-time-input" label="At time"
          required {...validation.field("time")} extra={cadence}>
          <Input id="schedule-time-input" data-testid="schedule-time" type="time" step={60} status={status("time")}
            value={draft.time} onChange={(event) => update({ time: event.target.value })} />
        </FormField>}
        {draft.frequency === "custom" && <FormField htmlFor="schedule-cron-input" label="Cron expression" required
          {...validation.field("cron")} help="Five fields: minute, hour, day of month, month, day of week.">
          <Input id="schedule-cron-input" data-testid="schedule-cron" placeholder="e.g. 0 9 * * *" status={status("cron")}
            value={draft.cron} onChange={(event) => update({ cron: event.target.value })} />
        </FormField>}
      </FieldRow>
      <FormField htmlFor="schedule-prompt-input" label="Prompt" required {...validation.field("prompt")}
        help="The agent gets this message on each run.">
        <Input.TextArea id="schedule-prompt-input" data-testid="schedule-prompt" rows={5} placeholder="Tell the agent what to do on each run" status={status("prompt")}
          value={draft.prompt} onChange={(event) => update({ prompt: event.target.value })} />
      </FormField>
      <FieldRow>
        <FormField htmlFor="schedule-timeout-input" label="Execution timeout" required {...validation.field("timeoutSeconds")}
          help="Time limit in seconds. It covers queueing and agent startup.">
          <InputNumber id="schedule-timeout-input" data-testid="schedule-timeout" placeholder="Seconds, e.g. 900" min={0.000001} max={TIMEOUT_MAX_SECONDS}
            status={status("timeoutSeconds")} value={draft.timeoutSeconds}
            onChange={(timeoutSeconds) => update({ timeoutSeconds })} />
        </FormField>
        <FormField htmlFor="schedule-enabled" label="Enabled" required={false}
          /* The one thing on screen that says whether creating or saving starts it
             running, so it carries an id of its own rather than being read as prose. */
          help={<span data-testid="schedule-enabled-note">
            {`This schedule will ${draft.enabled ? "run" : "not run"} automatically after it is ${schedule ? "saved" : "created"}.`}
          </span>}>
          <Switch id="schedule-enabled" data-testid="schedule-enabled" checked={draft.enabled}
            onChange={(enabled) => update({ enabled })} />
        </FormField>
      </FieldRow>
    </Form>
    <FormActions
      testIdPrefix="schedule"
      blocked={!validation.valid}
      submitLabel={schedule ? "Save" : "Create"}
      onSubmit={() => void submit()}
      submitting={saving}
      cancel={{ to: cancelTo }}
      error={failure !== undefined ? <SubmitError
        error={failure}
        what="schedule"
        outcome={schedule ? "saved" : "created"}
        onRetry={() => void submit()}
        data-testid="schedule-form-error" /> : undefined}
    />
    <UnsavedChangesModal blocker={blocker} testIdPrefix="schedule" />
  </div>;
}
