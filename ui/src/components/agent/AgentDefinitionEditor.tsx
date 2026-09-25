import { useState } from "react";
import { Alert, Form, Input, Modal, Radio, Select } from "antd";
import {
  apiClient,
  useNamespaces,
  type Agent,
  type AgentResource,
  type AgentTemplateSpec,
} from "@/api";
import type { HarnessSpec } from "@/api/domain/harnesses";

type Mode = "reference" | "inline";

/** Both choices are independent; inline values are complete specs, not patches. */
export function AgentDefinitionEditor({
  agent,
  onClose,
  onSaved,
}: {
  agent?: Agent;
  onClose: () => void;
  onSaved: (agent: Agent) => void;
}) {
  const namespaces = useNamespaces();
  const [namespace, setNamespace] = useState(agent?.namespace ?? "");
  const [name, setName] = useState(agent?.name ?? "");
  const spec = agent?.resource.spec;
  const [templateMode, setTemplateMode] = useState<Mode>(spec?.template ? "inline" : "reference");
  const [harnessMode, setHarnessMode] = useState<Mode>(spec?.harness ? "inline" : "reference");
  const [templateRef, setTemplateRef] = useState(spec?.templateRef?.name ?? "");
  const [harnessRef, setHarnessRef] = useState(spec?.harnessRef?.name ?? "");
  const [template, setTemplate] = useState(JSON.stringify(spec?.template ?? {}, null, 2));
  const [harness, setHarness] = useState(JSON.stringify(spec?.harness ?? {}, null, 2));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();

  async function save() {
    setSaving(true);
    setError(undefined);
    try {
      const resource: AgentResource = {
        metadata: { ...agent?.resource.metadata, namespace, name },
        spec: {
          ...(templateMode === "reference"
            ? { templateRef: { name: templateRef.trim() } }
            : { template: parseSpec<AgentTemplateSpec>(template) }),
          ...(harnessMode === "reference"
            ? { harnessRef: { name: harnessRef.trim() } }
            : { harness: parseSpec<HarnessSpec>(harness) }),
        },
      };
      const input = { namespace, name, resource };
      const saved = agent
        ? await apiClient.agentBuildingBlocks.updateAgent(input)
        : await apiClient.agentBuildingBlocks.createAgent(input);
      onSaved(saved);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  const incomplete = !namespace || !name
    || (templateMode === "reference" && !templateRef.trim())
    || (harnessMode === "reference" && !harnessRef.trim());

  return (
    <Modal
      open
      title={agent ? "Edit Agent" : "New Agent"}
      onCancel={onClose}
      onOk={() => void save()}
      okText={agent ? "Save Agent" : "Create Agent"}
      confirmLoading={saving}
      okButtonProps={{ disabled: incomplete }}
      width={720}
    >
      <Form layout="vertical">
        <Form.Item label="Namespace" htmlFor="agent-namespace" required>
          <Select
            id="agent-namespace"
            value={namespace || undefined}
            onChange={setNamespace}
            disabled={!!agent}
            options={(namespaces.data ?? []).map((row) => ({ value: row.name }))}
            loading={namespaces.isLoading}
          />
        </Form.Item>
        {namespaces.error && (
          <Alert type="error" title="Could not load namespaces" description={namespaces.error.message} />
        )}
        <Form.Item label="Name" htmlFor="agent-name" required>
          <Input
            id="agent-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            disabled={!!agent}
          />
        </Form.Item>
        <Form.Item
          label="Template"
          extra="Reference an AgentTemplate in this namespace, or embed its complete spec."
        >
          <Radio.Group
            aria-label="Template source"
            value={templateMode}
            onChange={(event) => setTemplateMode(event.target.value)}
            options={[{ value: "reference", label: "Reference" }, { value: "inline", label: "Inline" }]}
          />
        </Form.Item>
        <Form.Item
          label={templateMode === "reference" ? "AgentTemplate name" : "Template spec (JSON)"}
          htmlFor="agent-template"
          required
        >
          {templateMode === "reference" ? (
            <Input id="agent-template" value={templateRef} onChange={(event) => setTemplateRef(event.target.value)} />
          ) : (
            <Input.TextArea
              id="agent-template"
              value={template}
              onChange={(event) => setTemplate(event.target.value)}
              autoSize={{ minRows: 6, maxRows: 16 }}
            />
          )}
        </Form.Item>
        <Form.Item
          label="Harness"
          extra="Reference a Harness in this namespace, or embed its complete spec."
        >
          <Radio.Group
            aria-label="Harness source"
            value={harnessMode}
            onChange={(event) => setHarnessMode(event.target.value)}
            options={[{ value: "reference", label: "Reference" }, { value: "inline", label: "Inline" }]}
          />
        </Form.Item>
        <Form.Item
          label={harnessMode === "reference" ? "Harness name" : "Harness spec (JSON)"}
          htmlFor="agent-harness"
          required
        >
          {harnessMode === "reference" ? (
            <Input id="agent-harness" value={harnessRef} onChange={(event) => setHarnessRef(event.target.value)} />
          ) : (
            <Input.TextArea
              id="agent-harness"
              value={harness}
              onChange={(event) => setHarness(event.target.value)}
              autoSize={{ minRows: 6, maxRows: 16 }}
            />
          )}
        </Form.Item>
        {error && <Alert type="error" title="Could not save Agent" description={error} />}
      </Form>
    </Modal>
  );
}

function parseSpec<T>(text: string): T {
  const parsed: unknown = JSON.parse(text);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("An inline spec must be a JSON object.");
  }
  return parsed as T;
}
