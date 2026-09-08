import { useState } from "react";
import { Alert, AutoComplete, Checkbox, Form, Input, InputNumber, Modal, Select, Switch, Typography } from "antd";
import { fromJson } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { agentPairsFrom, newConversationBlockedReason, useAgentTemplatesAcrossNamespaces, useNamespaces } from "@/api";
import { invoke } from "@/api/operations";
import type { ScheduledRun } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
import { minuteIntervals, parseSchedule, scheduleCron, scheduleDescription, weekdays, type ScheduleTiming } from "./scheduleTiming";

const timeZones = ["UTC", ...Intl.supportedValuesOf("timeZone")].map((value) => ({ value }));

interface FormValues extends ScheduleTiming {
  agent: string;
  name: string;
  timeZone: string;
  prompt: string;
  enabled: boolean;
  timeoutSeconds: number;
}

export function ScheduledRunForm({ schedule, onClose, onSaved }: {
  schedule?: ScheduledRun;
  onClose: () => void;
  onSaved: (schedule: ScheduledRun) => void;
}) {
  const [form] = Form.useForm<FormValues>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  // Retain the key after a failed response: retrying must not create another schedule.
  const [requestId] = useState(() => crypto.randomUUID());
  const namespaces = useNamespaces();
  const templates = useAgentTemplatesAcrossNamespaces(schedule ? undefined : namespaces.data?.map((row) => row.name));
  const agents = agentPairsFrom(templates.data?.templates ?? []);
  const config = schedule?.config;
  const initialTiming = parseSchedule(config?.schedule ?? "0 9 * * *");
  const watched = Form.useWatch([], form) as FormValues | undefined;
  const timing = { ...initialTiming, ...watched };
  const initialTimeout = config?.executionTimeout
    ? Number(config.executionTimeout.seconds) + config.executionTimeout.nanos / 1e9 : 900;
  const loadError = namespaces.error ?? templates.error;

  async function save(values: FormValues) {
    setSaving(true);
    setError(undefined);
    try {
      const cron = scheduleCron({ ...initialTiming, ...values });
      const nextConfig = {
        ...config,
        name: values.name?.trim() ?? "",
        // Preserve existing expressions when only unrelated fields were edited.
        schedule: config && cron === scheduleCron(initialTiming) ? config.schedule : cron,
        timeZone: values.timeZone?.trim() || "UTC",
        prompt: values.prompt,
        paused: !values.enabled,
        executionTimeout: config?.executionTimeout && values.timeoutSeconds === initialTimeout
          ? config.executionTimeout : fromJson(DurationSchema, `${values.timeoutSeconds}s`),
      };
      let saved: ScheduledRun | undefined;
      if (schedule) {
        saved = (await invoke("scheduledRuns.update", {
          scheduledRunId: schedule.id, etag: schedule.etag, config: nextConfig,
        })).scheduledRun;
      } else {
        const agent = agents.find((entry) => entry.id === values.agent);
        if (!agent) throw new Error("Choose an available agent.");
        saved = (await invoke("scheduledRuns.create", {
          requestId,
          harness: { namespace: agent.namespace, name: agent.harness },
          agentTemplate: { namespace: agent.namespace, name: agent.agentTemplate },
          config: nextConfig,
        })).scheduledRun;
      }
      if (!saved) throw new Error("The API returned no schedule.");
      onSaved(saved);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  return <Modal open title={schedule ? "Edit schedule" : "New schedule"}
    styles={{ body: { maxHeight: "calc(100dvh - 240px)", overflowY: "auto" } }}
    onCancel={onClose} onOk={() => form.submit()} okText={schedule ? "Save changes" : "Create schedule"}
    confirmLoading={saving} cancelButtonProps={{ disabled: saving }} closable={!saving}
    mask={{ closable: !saving }}>
    <Form form={form} layout="vertical" onFinish={save} disabled={saving} initialValues={{
      ...initialTiming, name: config?.name ?? "",
      timeZone: config?.timeZone || "UTC", prompt: config?.prompt ?? "",
      enabled: !(config?.paused ?? false), timeoutSeconds: initialTimeout,
    }}>
      {error && <Alert type="error" showIcon title="Could not save schedule" description={error} />}
      {!schedule && <>
        {loadError && <Alert type="error" showIcon title="Could not load agents" description={loadError.message} />}
        {templates.data?.refused.map((entry) => <Alert key={entry.namespace} type="warning" showIcon
          title={`Could not read agents in ${entry.namespace}`} description={entry.reason} />)}
        <Form.Item name="agent" label="Agent" rules={[{ required: true, message: "Choose an agent." }]}>
          <Select showSearch={{ optionFilterProp: "label" }} loading={namespaces.isLoading || templates.isLoading}
            placeholder="Choose an agent" options={agents.map((agent) => {
              const blocked = newConversationBlockedReason(agent);
              return { value: agent.id, disabled: !!blocked,
                label: `${agent.namespace}/${agent.agentTemplate} on ${agent.harness}${blocked ? ` — ${blocked}` : ""}` };
            })} />
        </Form.Item>
      </>}
      <Form.Item name="name" label="Schedule Name" rules={[{ max: 200 }]}><Input maxLength={200} /></Form.Item>
      <Form.Item name="timeZone" label="Time zone" extra="The schedule uses this time zone, including daylight-saving changes.">
        <AutoComplete options={timeZones} placeholder="UTC" maxLength={253}
          filterOption={(input, option) => !!option?.value.toLowerCase().includes(input.toLowerCase())} />
      </Form.Item>
      <Form.Item name="frequency" label="Repeat">
        <Select options={[
          { value: "minutes", label: "Every few minutes" },
          { value: "hourly", label: "Hourly" },
          { value: "daily", label: "Daily" },
          { value: "weekly", label: "Weekly" },
          { value: "monthly", label: "Monthly" },
          { value: "custom", label: "Custom (advanced)" },
        ]} onChange={(frequency) => {
          if (frequency === "custom") form.setFieldValue("cron", scheduleCron(timing));
        }} />
      </Form.Item>
      {timing.frequency === "minutes" && <Form.Item name="interval" label="Every" rules={[{ required: true }]}>
        <Select options={minuteIntervals.map((value) => ({ value, label: `${value} minute${value === 1 ? "" : "s"}` }))} />
      </Form.Item>}
      {timing.frequency === "hourly" && <Form.Item name="minute" label="Minute past the hour"
        rules={[{ required: true }, { type: "integer", min: 0, max: 59 }]}>
        <InputNumber min={0} max={59} precision={0} />
      </Form.Item>}
      {timing.frequency === "weekly" && <Form.Item name="days" label="On days"
        rules={[{ type: "array", min: 1, required: true, message: "Choose at least one day." }]}>
        <Checkbox.Group options={[1, 2, 3, 4, 5, 6, 0].map((value) => ({ value, label: weekdays[value] }))} />
      </Form.Item>}
      {timing.frequency === "monthly" && <Form.Item name="monthDay" label="Day of the month"
        extra="Months without this day are skipped."
        rules={[{ required: true }, { type: "integer", min: 1, max: 31 }]}>
        <InputNumber min={1} max={31} precision={0} />
      </Form.Item>}
      {["daily", "weekly", "monthly"].includes(timing.frequency) && <Form.Item name="time" label="At time"
        rules={[{ required: true, message: "Choose a time." }]}>
        <Input type="time" step={60} />
      </Form.Item>}
      {timing.frequency === "custom" && <Form.Item name="cron" label="Cron expression"
        extra="Five fields: minute, hour, day of month, month, day of week."
        rules={[{ required: true, whitespace: true }, { max: 256 }]}><Input /></Form.Item>}
      {timing.frequency !== "custom" && timing.time && timing.days.length > 0 && timing.minute != null && timing.monthDay != null && <Typography.Paragraph type="secondary" role="status">
        {scheduleDescription(scheduleCron(timing))} ({(watched?.timeZone ?? config?.timeZone)?.trim() || "UTC"})
      </Typography.Paragraph>}
      <Form.Item name="prompt" label="Prompt" rules={[{ required: true, whitespace: true }, {
        validator: (_, value: string | undefined) => new TextEncoder().encode(value ?? "").length <= 32768
          ? Promise.resolve() : Promise.reject(new Error("Prompt must be at most 32 KiB.")),
      }]}><Input.TextArea rows={5} /></Form.Item>
      <Form.Item name="timeoutSeconds" label="Execution timeout (seconds)" extra="Includes queueing and agent startup."
        rules={[{ required: true }, { type: "number", min: 0.000001, max: 9223372036 }]}>
        <InputNumber min={0.000001} max={9223372036} />
      </Form.Item>
      <Form.Item name="enabled" label="Enable Schedule" valuePropName="checked"
        extra={`This schedule will ${(watched?.enabled ?? !(config?.paused ?? false)) ? "run" : "not run"} automatically after it is ${schedule ? "saved" : "created"}.`}>
        <Switch />
      </Form.Item>
    </Form>
  </Modal>;
}
