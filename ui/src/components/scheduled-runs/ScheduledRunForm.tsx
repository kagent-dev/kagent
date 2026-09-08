import { useState } from "react";
import { Alert, Form, Input, InputNumber, Modal, Select, Switch } from "antd";
import { fromJson } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { agentPairsFrom, newConversationBlockedReason, useAgentTemplatesAcrossNamespaces, useNamespaces } from "@/api";
import { invoke } from "@/api/operations";
import type { ScheduledRun } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";

interface FormValues {
  agent: string;
  name: string;
  schedule: string;
  timeZone: string;
  prompt: string;
  paused: boolean;
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
  const initialTimeout = config?.executionTimeout
    ? Number(config.executionTimeout.seconds) + config.executionTimeout.nanos / 1e9 : 900;
  const loadError = namespaces.error ?? templates.error;

  async function save(values: FormValues) {
    setSaving(true);
    setError(undefined);
    try {
      const nextConfig = {
        ...config,
        name: values.name?.trim() ?? "",
        schedule: values.schedule.trim(),
        timeZone: values.timeZone?.trim() || "UTC",
        prompt: values.prompt,
        paused: values.paused,
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
    onCancel={onClose} onOk={() => form.submit()} okText={schedule ? "Save changes" : "Create schedule"}
    confirmLoading={saving} cancelButtonProps={{ disabled: saving }} closable={!saving}
    mask={{ closable: !saving }}>
    <Form form={form} layout="vertical" onFinish={save} disabled={saving} initialValues={{
      name: config?.name ?? "", schedule: config?.schedule ?? "0 9 * * *",
      timeZone: config?.timeZone || "UTC", prompt: config?.prompt ?? "",
      paused: config?.paused ?? false, timeoutSeconds: initialTimeout,
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
      <Form.Item name="name" label="Name" rules={[{ max: 200 }]}><Input maxLength={200} /></Form.Item>
      <Form.Item name="schedule" label="Cron expression" extra="Five fields: minute, hour, day of month, month, day of week. Example: 0 9 * * * runs daily at 09:00."
        rules={[{ required: true, whitespace: true }, { max: 256 }]}><Input /></Form.Item>
      <Form.Item name="timeZone" label="Time zone" extra="Use an IANA time zone, such as America/New_York. Defaults to UTC.">
        <Input placeholder="UTC" maxLength={253} />
      </Form.Item>
      <Form.Item name="prompt" label="Prompt" rules={[{ required: true, whitespace: true }, {
        validator: (_, value: string | undefined) => new TextEncoder().encode(value ?? "").length <= 32768
          ? Promise.resolve() : Promise.reject(new Error("Prompt must be at most 32 KiB.")),
      }]}><Input.TextArea rows={5} /></Form.Item>
      <Form.Item name="timeoutSeconds" label="Execution timeout (seconds)" extra="Includes queueing and agent startup."
        rules={[{ required: true }, { type: "number", min: 0.000001, max: 9223372036 }]}>
        <InputNumber min={0.000001} max={9223372036} />
      </Form.Item>
      <Form.Item name="paused" label="Paused" valuePropName="checked"><Switch /></Form.Item>
    </Form>
  </Modal>;
}
