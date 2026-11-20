import { useMemo, useState } from "react";
import { flushSync } from "react-dom";
import { Form } from "antd";
import { apiClient, useInvalidateHarnesses, useNamespaces, useWorkerPools, type Harness, type HarnessSpec } from "@/api";
import { DEFAULT_HARNESS_WORKER_POOL } from "@/api/domain/harnesses";
import { SubmitError } from "@/components/common/SubmitError";
import { isValidNamespace, isValidObjectName, OBJECT_NAME_HINT } from "@/components/common/resourceName";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { FieldRow } from "@/components/form/FieldRow";
import { NameInput } from "@/components/form/NameInput";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { toastSaved } from "@/components/form/saveToast";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation, type FieldIssue } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import { copyName } from "@/components/table/copyFrom";
import { HarnessFields } from "./HarnessFields";
import {
  emptyHarnessDraft,
  harnessDraftFromSpec,
  harnessDraftIssues,
  harnessSpecFromDraft,
  type HarnessDraft,
  type HarnessDraftField,
} from "./harnessDraft";

type HarnessField = "name" | "namespace" | HarnessDraftField;

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
  cancel,
  inDialog = false,
}: {
  harness?: Harness;
  /**
   * Prefills a create from this harness, in its namespace; nothing of it is edited.
   * Not combinable with `namespace`: a copy pinned to another namespace would keep
   * references to resources that only exist in the source's.
   */
  copyOf?: Harness;
  namespace?: string;
  onSaved: (saved: Harness) => void;
  cancel: { to: string } | { onClick: () => void };
  /** In a dialog the page around it owns the unsaved-changes guard; a router allows one. */
  inDialog?: boolean;
} & ({ copyOf?: undefined } | { namespace?: undefined })) {
  const namespaces = useNamespaces();
  const invalidateHarnesses = useInvalidateHarnesses();
  const existing = (harness ?? copyOf)?.resource.spec as HarnessSpec | undefined;

  const fallbackNamespace = useMemo(() => {
    const names = (namespaces.data ?? []).map((row) => row.name);
    return names.includes("kagent") ? "kagent" : names[0];
  }, [namespaces.data]);
  const [chosenNamespace, setNamespace] = useState(copyOf?.namespace);
  const namespace = harness?.namespace ?? fixedNamespace ?? chosenNamespace ?? fallbackNamespace;
  const [name, setName] = useState(harness?.name ?? (copyOf ? copyName(copyOf.name) : ""));
  const [rawDraft, setDraft] = useState(() => (existing ? harnessDraftFromSpec(existing) : emptyHarnessDraft()));
  const [poolTouched, setPoolTouched] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  // The chart installs `kagent-default`: an untouched empty pool shows it where it exists.
  const pools = useWorkerPools(namespace);
  const poolNames = (pools.data ?? []).map((pool) => pool.name);
  const defaultPool = poolNames.includes(DEFAULT_HARNESS_WORKER_POOL) ? DEFAULT_HARNESS_WORKER_POOL : "";
  const withDefaultPool = (of: HarnessDraft) =>
    of.workerPool === "" && !poolTouched ? { ...of, workerPool: defaultPool } : of;
  const draft = withDefaultPool(rawDraft);

  // Compared by value against the first render, with the same default pool applied to both.
  const [baseline] = useState(() => ({ namespace: chosenNamespace, name, draft: rawDraft }));
  const [saved, setSaved] = useState(false);
  const isDirty =
    !saved &&
    JSON.stringify([chosenNamespace, name, draft]) !==
      JSON.stringify([baseline.namespace, baseline.name, withDefaultPool(baseline.draft)]);

  const issues = useMemo(() => {
    const list: FieldIssue<HarnessField>[] = [];
    const trimmed = name.trim();
    if (!harness) {
      if (trimmed === "") list.push({ field: "name", message: "A name is required." });
      else if (!isValidObjectName(trimmed)) {
        list.push({ field: "name", message: OBJECT_NAME_HINT });
      }
      if (!namespace) list.push({ field: "namespace", message: "A namespace is required." });
      else if (!isValidNamespace(namespace)) {
        list.push({ field: "namespace", message: "The namespace must be a valid Kubernetes name." });
      }
    }
    return [...list, ...harnessDraftIssues(draft)];
  }, [draft, harness, name, namespace]);
  const validation = useSubmitValidation(issues);

  async function save() {
    setFailure(undefined);
    if (!validation.attempt() || !namespace) return;
    setSaving(true);
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
      const result = harness
        ? await apiClient.agentBuildingBlocks.updateHarness(input)
        : await apiClient.agentBuildingBlocks.createHarness(input);
      // Refreshes any list still on screen. Guarded, not load-bearing.
      await invalidateHarnesses().catch(() => {});
      toastSaved(harness ? "saved" : "created", "harness", result.name);
      // Committed before the caller navigates, so the guard lets the save's own navigation through.
      flushSync(() => setSaved(true));
      onSaved(result);
    } catch (cause: unknown) {
      setFailure(cause);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Form layout="vertical" component="div">
      <FieldRow>
        <FormField
          label="Name"
          required
          htmlFor="harness-name"
          {...validation.field("name")}
          help={harness ? "You can't change the name." : "Pick a name to refer to this harness."}
        >
          <NameInput
            dots
            id="harness-name"
            data-testid="harness-name"
            value={name}
            disabled={Boolean(harness)}
            onChange={setName}
            placeholder="my-harness"
          />
        </FormField>

        <FormField
          label="Namespace"
          required
          htmlFor="harness-namespace"
          {...validation.field("namespace")}
          extra={
            harness
              ? "Fixed once the harness exists."
              : fixedNamespace
                ? "The agent's namespace: an agent can only use a harness in its own."
                : "Pick the namespace to create the harness in."
          }
        >
          <NamespaceSelect
            id="harness-namespace"
            data-testid="harness-namespace"
            value={namespace}
            disabled={Boolean(harness || fixedNamespace)}
            onChange={setNamespace}
          />
        </FormField>
      </FieldRow>

      <HarnessFields
        draft={draft}
        onChange={(next) => {
          if (next.workerPool !== draft.workerPool) setPoolTouched(true);
          setDraft(next);
          setFailure(undefined);
        }}
        pools={{ names: poolNames, loading: pools.isLoading, error: pools.error?.message }}
        fieldFor={validation.field}
      />

      <FormActions
        submitLabel={harness ? "Save" : "Create"}
        onSubmit={() => void save()}
        submitting={saving}
        cancel={cancel}
        testIdPrefix="harness"
        blocked={!validation.valid}
        error={
          failure !== undefined ? (
            <SubmitError
              error={failure}
              what="harness"
              outcome={harness ? "saved" : "created"}
              onRetry={() => void save()}
              data-testid="harness-error"
            />
          ) : null
        }
      />

      {inDialog ? null : <HarnessUnsavedGuard isDirty={isDirty} />}
    </Form>
  );
}

function HarnessUnsavedGuard({ isDirty }: { isDirty: boolean }) {
  const { blocker } = useUnsavedChangesGuard(isDirty);
  return (
    <UnsavedChangesModal blocker={blocker} testIdPrefix="harness" />
  );
}
