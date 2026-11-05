import { useMemo, useState } from "react";
import { Alert, Button, Form, Input, Select, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Plus } from "lucide-react";
import { EGRESS_LIMITS, isApiError, useNamespaces, type EgressPolicyResource } from "@/api";
import { SubmitError } from "@/components/common/SubmitError";
import { DestinationEditor } from "./DestinationEditor";
import { egressCopy } from "./egressPolicyCopy";
import {
  draftIssues,
  emptyDestination,
  resourceFromDraft,
  type EgressPolicyDraft,
} from "./egressPolicyDraft";

const { Text } = Typography;

// region Helpers

/** `Aborted` is the server saying our `resourceVersion` is stale. */
function isStale(error: unknown): boolean {
  return isApiError(error) && error.code === "Aborted";
}

// region Component

/** Fields and rules for an egress policy. The caller owns the request. */
export function EgressPolicyForm({
  initial,
  existing,
  onSubmit,
  onCancel,
  onReload,
}: {
  initial: EgressPolicyDraft;
  /** The loaded resource on an edit; its metadata goes back with the save. */
  existing?: EgressPolicyResource;
  onSubmit: (resource: EgressPolicyResource) => Promise<void>;
  onCancel: () => void;
  /** Re-reads the policy after a stale save. */
  onReload?: () => void;
}) {
  const theme = useTheme();
  const namespaces = useNamespaces();
  const editing = Boolean(existing);
  const [draft, setDraft] = useState(initial);
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const issues = useMemo(() => draftIssues(draft), [draft]);

  const update = (patch: Partial<EgressPolicyDraft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    setFailure(undefined);
  };

  async function submit() {
    setSubmitted(true);
    setFailure(undefined);
    if (issues.count > 0) return;
    setSaving(true);
    try {
      await onSubmit(resourceFromDraft(draft, existing));
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  }

  const full = draft.destinations.length >= EGRESS_LIMITS.destinations;

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 860 }}>
      <Form
        layout="vertical"
        component="div"
        css={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", columnGap: theme.space(4) }}
      >
        <Form.Item label={egressCopy.form.name} required={!editing} extra={editing ? egressCopy.form.identityLocked : undefined}>
          <Input
            data-testid="egress-form-name"
            value={draft.name}
            readOnly={editing}
            placeholder="github-api"
            onChange={(event) => update({ name: event.target.value })}
            css={{ fontFamily: theme.font.mono }}
          />
        </Form.Item>
        <Form.Item label={egressCopy.form.namespace} required={!editing} extra={editing ? egressCopy.form.identityLocked : undefined}>
          <Select
            data-testid="egress-form-namespace"
            value={draft.namespace || undefined}
            disabled={editing}
            loading={namespaces.isLoading}
            showSearch
            onChange={(namespace: string) => update({ namespace })}
            options={(namespaces.data ?? []).map((entry) => ({ value: entry.name, label: entry.name }))}
          />
        </Form.Item>
      </Form>

      <div>
        <Text strong css={{ display: "block" }}>{egressCopy.form.destinations}</Text>
        <Text css={{ display: "block", color: theme.color.textMuted }}>
          {egressCopy.form.destinationsHelp(EGRESS_LIMITS.destinations)}
        </Text>
      </div>

      {draft.destinations.map((row, index) => (
        <DestinationEditor
          key={row.id}
          index={index}
          row={row}
          disabled={saving}
          problems={submitted ? issues.destinations[row.id] : undefined}
          onChange={(next) =>
            update({ destinations: draft.destinations.map((entry) => (entry.id === row.id ? next : entry)) })
          }
          onRemove={
            draft.destinations.length > 1
              ? () => update({ destinations: draft.destinations.filter((entry) => entry.id !== row.id) })
              : undefined
          }
        />
      ))}

      <Button
        icon={<Plus size={14} />}
        data-testid="egress-form-add-destination"
        disabled={full || saving}
        onClick={() => update({ destinations: [...draft.destinations, emptyDestination()] })}
        css={{ alignSelf: "flex-start" }}
      >
        {egressCopy.form.addDestination}
      </Button>

      {submitted && issues.policy.length > 0 ? (
        <Alert
          type="error"
          showIcon
          title={egressCopy.form.issuesTitle}
          data-testid="egress-form-errors"
          description={
            <ul css={{ margin: 0, paddingLeft: theme.space(5) }}>
              {issues.policy.map((issue) => (
                <li key={issue}>{issue}</li>
              ))}
            </ul>
          }
        />
      ) : null}

      {failure !== undefined && isStale(failure) ? (
        <Alert
          type="warning"
          showIcon
          title={egressCopy.form.staleTitle}
          description={egressCopy.form.staleBody}
          data-testid="egress-form-stale"
          action={
            onReload ? (
              <Button size="small" onClick={onReload} data-testid="egress-form-reload">
                {egressCopy.form.reload}
              </Button>
            ) : undefined
          }
        />
      ) : failure !== undefined ? (
        <SubmitError
          error={failure}
          what={egressCopy.kind}
          outcome={editing ? "saved" : "created"}
          onRetry={() => void submit()}
          data-testid="egress-form-submit-error"
        />
      ) : null}

      <Space size={8} wrap>
        <Button type="primary" data-testid="egress-form-submit" loading={saving} onClick={() => void submit()}>
          {editing ? egressCopy.form.save : egressCopy.form.create}
        </Button>
        <Button onClick={onCancel} data-testid="egress-form-cancel">
          {egressCopy.form.cancel}
        </Button>
      </Space>
    </Space>
  );
}
