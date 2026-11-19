import { useEffect, useMemo, useRef, useState } from "react";
import { Alert, Button, Form, Input, Typography, type InputRef } from "antd";
import { useTheme } from "@emotion/react";
import { EGRESS_LIMITS, isApiError, type EgressPolicy, type EgressPolicyResource } from "@/api";
import { SubmitError } from "@/components/common/SubmitError";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { FormRow, FormRowList } from "@/components/form/FormRowList";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import { DestinationEditor } from "./DestinationEditor";
import { egressCopy } from "./egressPolicyCopy";
import {
  draftFieldIssues,
  emptyDestination,
  resourceFromDraft,
  type EgressPolicyDraft,
} from "./egressPolicyDraft";

const { Text } = Typography;

/** `Aborted` is the server saying our `resourceVersion` is stale. */
function isStale(error: unknown): boolean {
  return isApiError(error) && error.code === "Aborted";
}

/** The edited values, without row ids, for telling whether anything changed. */
const contents = (draft: EgressPolicyDraft) => JSON.stringify(draft, (key, value) => (key === "id" ? undefined : value));

/** Fields and rules for an egress policy. The caller owns the request. */
export function EgressPolicyForm({
  initial,
  existing,
  onSubmit,
  onSaved,
  cancelTo,
  onReload,
}: {
  initial: EgressPolicyDraft;
  /** The loaded resource on an edit; its metadata goes back with the save. */
  existing?: EgressPolicyResource;
  onSubmit: (resource: EgressPolicyResource) => Promise<EgressPolicy>;
  /** Called with the saved policy once the guard is released; usually navigates. */
  onSaved: (policy: EgressPolicy) => void;
  /** Where Cancel goes. */
  cancelTo: string;
  /** Re-reads the policy after a stale save. */
  onReload?: () => void;
}) {
  const theme = useTheme();
  const editing = Boolean(existing);
  const [draft, setDraft] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [baseline] = useState(() => contents(initial));
  const issues = useMemo(() => draftFieldIssues(draft), [draft]);
  const validation = useSubmitValidation(issues);
  const { blocker, release } = useUnsavedChangesGuard(contents(draft) !== baseline);
  const container = useRef<HTMLDivElement>(null);
  const pendingOrigin = useRef<number>(undefined);
  const focusAdd = useRef(false);
  const originInputs = useRef(new Map<number, InputRef>());

  const update = (patch: Partial<EgressPolicyDraft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    setFailure(undefined);
  };

  async function submit() {
    setFailure(undefined);
    if (!validation.attempt()) return;
    setSaving(true);
    try {
      const policy = await onSubmit(resourceFromDraft(draft, existing));
      release();
      onSaved(policy);
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  }

  const full = draft.destinations.length >= EGRESS_LIMITS.destinations;

  function addDestination() {
    const next = emptyDestination();
    update({ destinations: [...draft.destinations, next] });
    // The new row's input mounts on the next render and takes focus from its ref callback.
    pendingOrigin.current = next.id;
  }

  function removeDestination(index: number) {
    update({ destinations: draft.destinations.filter((_, at) => at !== index) });
    // The removed row's button is gone, so focus falls back to "Add destination".
    focusAdd.current = true;
  }

  useEffect(() => {
    if (!focusAdd.current) return;
    focusAdd.current = false;
    container.current?.querySelector<HTMLElement>('[data-testid="egress-destinations-add"]')?.focus();
  }, [draft.destinations]);

  return (
    <div ref={container} css={{ display: "flex", flexDirection: "column", gap: theme.space(4), maxWidth: 860 }}>
      <Form
        layout="vertical"
        component="div"
        css={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", columnGap: theme.space(4) }}
      >
        <FormField
          htmlFor="egress-form-name-input"
          label={egressCopy.form.name}
          required
          {...validation.field("name")}
          help={editing ? egressCopy.form.identityLocked : undefined}
        >
          <Input
            id="egress-form-name-input"
            data-testid="egress-form-name"
            value={draft.name}
            readOnly={editing}
            placeholder="github-api"
            status={validation.errorFor("name") ? "error" : undefined}
            onChange={(event) => update({ name: event.target.value })}
            css={{ fontFamily: theme.font.mono }}
          />
        </FormField>
        <FormField
          htmlFor="egress-form-namespace-input"
          label={egressCopy.form.namespace}
          required
          {...validation.field("namespace")}
          help={editing ? egressCopy.form.identityLocked : undefined}
        >
          <NamespaceSelect
            id="egress-form-namespace-input"
            data-testid="egress-form-namespace"
            value={draft.namespace}
            disabled={editing}
            onChange={(namespace) => update({ namespace })}
          />
        </FormField>
      </Form>

      <div>
        <Text strong css={{ display: "block" }}>{egressCopy.form.destinations}</Text>
        <Text css={{ display: "block", color: theme.color.textMuted }}>
          {egressCopy.form.destinationsHelp(EGRESS_LIMITS.destinations)}
        </Text>
      </div>

      <FormRowList
        data-testid="egress-destinations"
        addLabel={egressCopy.form.addDestination}
        onAdd={addDestination}
        addDisabled={full || saving}
      >
        {draft.destinations.map((row, index) => (
          <FormRow
            key={row.id}
            data-testid={`egress-destination-${index}`}
            removeLabel={egressCopy.form.removeDestination(index + 1)}
            removeDisabled={saving}
            onRemove={draft.destinations.length > 1 ? () => removeDestination(index) : undefined}
          >
            <DestinationEditor
              index={index}
              row={row}
              disabled={saving}
              fieldFor={validation.field}
              onChange={(next) =>
                update({ destinations: draft.destinations.map((entry) => (entry.id === row.id ? next : entry)) })
              }
              originRef={(input) => {
                if (!input) {
                  originInputs.current.delete(row.id);
                  return;
                }
                originInputs.current.set(row.id, input);
                if (pendingOrigin.current === row.id) {
                  pendingOrigin.current = undefined;
                  input.focus();
                }
              }}
            />
          </FormRow>
        ))}
      </FormRowList>
      {validation.errorFor("destinations") ? (
        <Text type="danger" data-testid="egress-destinations-error">{validation.errorFor("destinations")}</Text>
      ) : null}

      <FormActions
        testIdPrefix="egress-form"
        blocked={!validation.valid}
        submitLabel={editing ? egressCopy.form.save : egressCopy.form.create}
        onSubmit={() => void submit()}
        submitting={saving}
        cancel={{ to: cancelTo }}
        error={
          failure !== undefined && isStale(failure) ? (
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
          ) : undefined
        }
      />

      <UnsavedChangesModal blocker={blocker} testIdPrefix="egress-form">
        {egressCopy.form.discardBody}
      </UnsavedChangesModal>
    </div>
  );
}
