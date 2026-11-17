import { useMemo, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { Skeleton, Space } from "antd";
import { useNavigate, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { AgentTemplateForm } from "@/components/agent-template-form/AgentTemplateForm";
import {
  draftFromTemplate,
  emptyDraft,
  type AgentTemplateDraft,
  labelsFromDraft,
  specFromDraft,
  templateDraftIssues,
} from "@/components/agent-template-form/agentTemplateDraft";
import { SubmitError } from "@/components/common/SubmitError";
import { ValidationNotice } from "@/components/form/ValidationNotice";
import { BackLink } from "@/components/form/BackLink";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { toastSaved } from "@/components/form/saveToast";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import { copyName, copySource, useMountedBlank } from "@/components/table/copyFrom";
import { CopyNotice } from "@/components/table/CopyNotice";
import { agentTemplatesTab } from "@/router/routes";
import { apiClient, useAgentTemplate, useInvalidateAgentTemplates, useNamespaces } from "@/api";

/**
 * Create an agent template.
 *
 * The page owns the draft and the write; `AgentTemplateForm` owns the fields. That
 * split is what lets the same form be used here and inside the agent-create page's
 * inline panel — the two differ only in what a save *means*, and that decision
 * belongs to the surface, not the fields.
 */
export function AgentTemplateNewPage() {
  // `?from=namespace/name` opens the form prefilled as a copy of that template.
  const from = copySource(useSearchParams()[0]);
  const source = useAgentTemplate(from?.namespace, from?.name);
  // Only while there is nothing to show: a failed revalidation must not replace a form being edited.
  const copyFailed = Boolean(from && source.error && !source.data);
  const loaded = Boolean(source.data);
  const mountedBlank = useMountedBlank(from && `${from.namespace}/${from.name}`, !from || loaded || copyFailed, loaded);
  if (from && !source.data && !copyFailed)
    return (
      <PageFrame title="New agent template">
        <Skeleton active paragraph={{ rows: 8 }} />
      </PageFrame>
    );
  return (
    <TemplateCreateForm
      // Keyed on the requested source, not the loaded one: a source that only arrives on a later
      // revalidation must not remount the form and discard what has been typed meanwhile.
      key={from ? `${from.namespace}/${from.name}` : "new"}
      initial={source.data && { ...draftFromTemplate(source.data), name: copyName(source.data.name) }}
      copyNotice={<CopyNotice kind="agent template" from={from} error={source.error?.message} loaded={loaded} mountedBlank={mountedBlank} testId="template-copy-error" />}
    />
  );
}

/** `copyNotice` says, above a form that began blank, why the copy source was not applied. */
function TemplateCreateForm({ initial, copyNotice }: { initial?: AgentTemplateDraft; copyNotice?: ReactNode }) {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const namespaces = useNamespaces();

  const fallbackNamespace = useMemo(() => {
    const names = (namespaces.data ?? []).map((entry) => entry.name);
    return names.includes("kagent") ? "kagent" : (names[0] ?? "kagent");
  }, [namespaces.data]);

  const namespace = searchParams.get("namespace") ?? fallbackNamespace;
  const invalidateTemplates = useInvalidateAgentTemplates();
  const [draft, setDraft] = useState(() => initial ?? emptyDraft(namespace));
  const [isSubmitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  // The draft follows the URL or cluster namespace until the reader picks one.
  const effective = draft.namespace || namespace;
  const current = { ...draft, namespace: effective };
  const validation = useSubmitValidation(templateDraftIssues(current, { isCreate: true }));

  // Compared by value against the draft the form opened with; `saved` lets the post-create navigation through.
  const [baseline] = useState(() => JSON.stringify(draft));
  const [saved, setSaved] = useState(false);
  const blocker = useUnsavedChangesGuard(!saved && JSON.stringify(draft) !== baseline);

  const update = (next: AgentTemplateDraft) => {
    setDraft(next);
    setFailure(undefined);
  };

  async function create(): Promise<void> {
    setFailure(undefined);
    if (!validation.attempt()) return;
    setSubmitting(true);
    try {
      const created = await apiClient.agentBuildingBlocks.createAgentTemplate({
        namespace: effective,
        name: draft.name.trim(),
        resource: {
          metadata: {
            name: draft.name.trim(),
            namespace: effective,
            labels: labelsFromDraft(draft),
          },
          // No existing spec to merge onto: this is a create.
          spec: specFromDraft(current),
        },
      });
      // Refreshes any list still on screen. Guarded, not load-bearing.
      await invalidateTemplates().catch(() => {});
      toastSaved("created", "agent template", created.name);
      // The list narrows on `ns`; `paths.agentTemplates` is a redirect that drops the query.
      // Committed before navigating, so the guard lets the save's own navigation through.
      flushSync(() => setSaved(true));
      navigate(`${agentTemplatesTab}&ns=${encodeURIComponent(effective)}`);
    } catch (cause: unknown) {
      setFailure(cause);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <BackLink to={agentTemplatesTab} resource="Templates" />
      <PageFrame
        title="New agent template"
        description="What an agent does. A harness supplies the runtime; an agent is one of each."
      >
        <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 860 }}>
          {copyNotice}
          <AgentTemplateForm
            draft={current}
            onChange={update}
            isCreate
            namespace={effective}
            errorFor={validation.errorFor}
            namespaceField={
              <FormField label="Namespace" required error={validation.errorFor("namespace")}>
                <div data-testid="template-form-namespace">
                  <NamespaceSelect
                    value={effective}
                    // Local references resolve in the new namespace, so the model is cleared.
                    onChange={(value) => update({ ...draft, namespace: value, modelConfig: undefined })}
                  />
                </div>
              </FormField>
            }
          />

          <FormActions
            submitLabel="Create"
            onSubmit={() => void create()}
            submitting={isSubmitting}
            cancel={{ to: agentTemplatesTab }}
            testIdPrefix="template"
            error={
              failure !== undefined ? (
                <SubmitError
                  error={failure}
                  what="agent template"
                  onRetry={() => void create()}
                  data-testid="template-create-error"
                />
              ) : validation.issues.length > 0 ? <ValidationNotice data-testid="template-create-error" /> : null
            }
          />
        </Space>

        <UnsavedChangesModal blocker={blocker} testIdPrefix="template">
          This template has not been created. Leaving throws away what you entered.
        </UnsavedChangesModal>
      </PageFrame>
    </>
  );
}
