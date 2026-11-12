import { useState } from "react";
import { Alert, Button, Form, Input, Select, Space } from "antd";
import { useTheme } from "@emotion/react";
import { apiClient, useInvalidateHarnesses, useNamespaces, type Harness, type HarnessSpec } from "@/api";
import { copyName } from "@/components/table/copyFrom";
import { HarnessFields } from "./HarnessFields";
import {
  emptyHarnessDraft,
  harnessDraftFromSpec,
  harnessDraftProblems,
  harnessSpecFromDraft,
} from "./harnessDraft";

/**
 * Creates a harness, or edits `harness`. The caller decides where to go afterwards, so
 * the same form serves the harness pages and the agent form's inline dialog.
 *
 * `namespace` fixes the namespace on a create, as the agent form does: an agent can
 * only reference a harness in its own namespace.
 */
export function HarnessForm({
  harness,
  copyOf,
  namespace: fixedNamespace,
  onSaved,
  onCancel,
}: {
  harness?: Harness;
  /** Prefills a create from this harness, in its namespace; nothing of it is edited. */
  copyOf?: Harness;
  namespace?: string;
  onSaved: (saved: Harness) => void;
  onCancel: () => void;
}) {
  const theme = useTheme();
  const namespaces = useNamespaces();
  const invalidateHarnesses = useInvalidateHarnesses();
  const existing = (harness ?? copyOf)?.resource.spec as HarnessSpec | undefined;

  const [chosenNamespace, setNamespace] = useState(harness?.namespace ?? fixedNamespace ?? copyOf?.namespace);
  const namespace = harness?.namespace ?? fixedNamespace ?? chosenNamespace;
  const [name, setName] = useState(harness?.name ?? (copyOf ? copyName(copyOf.name) : ""));
  const [draft, setDraft] = useState(() => (existing ? harnessDraftFromSpec(existing) : emptyHarnessDraft()));
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string>();

  const ready = Boolean(namespace) && name.trim() !== "" && harnessDraftProblems(draft).length === 0;

  async function save() {
    if (!namespace || !ready) return;
    setSaving(true);
    setFailure(undefined);
    const input = {
      namespace,
      name: name.trim(),
      resource: {
        // An edit sends the loaded metadata back, so a stale resourceVersion fails the write.
        metadata: { ...harness?.resource.metadata, name: name.trim(), namespace },
        spec: harnessSpecFromDraft(draft, existing),
      },
    };
    try {
      const saved = harness
        ? await apiClient.agentBuildingBlocks.updateHarness(input)
        : await apiClient.agentBuildingBlocks.createHarness(input);
      // Refreshes any list still on screen. Guarded, not load-bearing.
      await invalidateHarnesses().catch(() => {});
      onSaved(saved);
    } catch (cause: unknown) {
      // The controller's own words: its CEL messages name the field that was wrong,
      // which is more use than anything this form could say about it.
      setFailure(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Form layout="vertical">
      <Form.Item label="Name" required extra={harness ? "A Kubernetes name, so it cannot be changed." : undefined}>
        <Input
          data-testid="harness-name"
          value={name}
          disabled={Boolean(harness)}
          onChange={(event) => setName(event.target.value)}
          placeholder="my-harness"
        />
      </Form.Item>

      <Form.Item
        label="Namespace"
        required
        extra={
          harness
            ? "Fixed once the harness exists."
            : fixedNamespace
              ? "The agent's namespace: an agent can only use a harness in its own."
              : undefined
        }
      >
        <Select
          data-testid="harness-namespace"
          placeholder="Choose a namespace"
          value={namespace}
          disabled={Boolean(harness || fixedNamespace)}
          onChange={setNamespace}
          loading={namespaces.isLoading}
          options={(namespaces.data ?? []).map((row) => ({ value: row.name, label: row.name }))}
        />
      </Form.Item>

      <HarnessFields draft={draft} onChange={setDraft} namespace={namespace} />

      {failure ? (
        <Alert
          type="error"
          showIcon
          data-testid="harness-error"
          title={harness ? "Could not save this harness" : "Could not create this harness"}
          description={failure}
          css={{ marginBottom: theme.space(4) }}
        />
      ) : null}

      <Space size={8}>
        <Button
          type="primary"
          data-testid={harness ? "harness-save" : "harness-create"}
          loading={saving}
          disabled={!ready}
          onClick={() => void save()}
        >
          {harness ? "Save harness" : "Create harness"}
        </Button>
        <Button data-testid="harness-cancel" onClick={onCancel}>
          Cancel
        </Button>
      </Space>
    </Form>
  );
}
