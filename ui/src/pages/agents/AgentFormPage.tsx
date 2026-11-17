import { useMemo, useState, type ReactNode } from "react";
import { ReadError } from "@/components/common/ReadError";
import { flushSync } from "react-dom";
import { Alert, Button, Card, Form, Input, Modal, Radio, Select, Skeleton, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { AgentTemplateForm } from "@/components/agent-template-form/AgentTemplateForm";
import {
  draftFromSpec,
  draftProblems,
  emptyDraft,
  specFromDraft,
} from "@/components/agent-template-form/agentTemplateDraft";
import { hasUnshownSpecFields } from "@/components/agent-template-form/unshownFields";
import { SubmitError } from "@/components/common/SubmitError";
import { ValidationNotice } from "@/components/form/ValidationNotice";
import { RFC1123_SUBDOMAIN } from "@/components/common/resourceName";
import { BackLink } from "@/components/form/BackLink";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import { toastSaved } from "@/components/form/saveToast";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation, type FieldIssue } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import {
  apiClient,
  useAgent,
  useAgentTemplates,
  useNamespaceEgressPolicies,
  useHarnesses,
  useInvalidateAgents,
  useModel,
  useNamespaces,
  type Agent,
  type AgentSpec,
} from "@/api";
import { copyName, copySource, useMountedBlank } from "@/components/table/copyFrom";
import { CopyNotice } from "@/components/table/CopyNotice";
import { paths } from "@/router/routes";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import { HarnessForm } from "@/components/harness-form/HarnessForm";
import { HarnessIntro } from "@/components/harness-form/HarnessFields";
import { harnessModelConflict, preferredHarness } from "@/components/harness-form/harnessChoice";

const { Text } = Typography;

type Source = "reference" | "inline";

const agentsTab = `${paths.agents}?tab=agents`;

/** Create or edit an Agent. Edit is the `agentEdit` route, whose name and namespace are fixed. */
export function AgentFormPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const editing = Boolean(namespace && name);
  const existing = useAgent(namespace, name);
  // `?from=namespace/name` opens a create prefilled as a copy of that agent.
  const from = copySource(useSearchParams()[0]);
  const copying = !editing && Boolean(from);
  const source = useAgent(copying ? from?.namespace : undefined, copying ? from?.name : undefined);
  const namespaces = useNamespaces();
  // The form mounts once namespaces arrive and, for a copy, once its source has answered.
  const formReady = !editing && Boolean(namespaces.data) && (!copying || Boolean(source.data || source.error));
  const mountedBlank = useMountedBlank(from && `${from.namespace}/${from.name}`, formReady, Boolean(source.data));

  return (
    <>
      <BackLink to={agentsTab} resource="Agents" />
      <PageFrame
        title={editing ? `Edit agent ${name}` : "New agent"}
        description="An agent pairs inline or shared behavior with a platform-managed harness."
      >
        {!editing && namespaces.error ? (
          <ReadError
            resource="namespaces"
            error={namespaces.error}
            testId="agent-form-load-error"
            onRetry={namespaces.refresh}
          />
        ) : formReady ? (
          // Mounted once namespaces arrive, so the ref lists never read an empty namespace.
          // A copy source that could not be had leaves a blank form, with the reason above it.
          <>
            {copying ? (
              <div css={{ maxWidth: 860 }}>
                <CopyNotice kind="agent" from={from} error={source.error?.message} loaded={Boolean(source.data)} mountedBlank={mountedBlank} testId="agent-copy-error" />
              </div>
            ) : null}
            <AgentForm
              // Keyed on the requested source, so a late-arriving one never remounts an edited form.
              key={from ? `${from.namespace}/${from.name}` : "new"}
              copyOf={source.data}
            />
          </>
        ) : editing && existing.error ? (
          <ReadError
            resource="this agent"
            error={existing.error}
            testId="agent-form-load-error"
            onRetry={existing.refresh}
          />
        ) : editing && existing.data ? (
          <AgentForm agent={existing.data} />
        ) : (
          <Card css={{ maxWidth: 860 }}>
            <Skeleton active paragraph={{ rows: 8 }} />
          </Card>
        )}
      </PageFrame>
    </>
  );
}

type AgentField = "name" | "namespace" | "harness" | "template";

function AgentForm({ agent, copyOf }: { agent?: Agent; copyOf?: Agent }) {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const invalidateAgents = useInvalidateAgents();
  const base = agent ?? copyOf;
  const spec = base?.resource.spec;

  const fallbackNamespace = useMemo(() => {
    const names = (namespaces.data ?? []).map((entry) => entry.name);
    return names.includes("kagent") ? "kagent" : (names[0] ?? "");
  }, [namespaces.data]);

  const [chosenNamespace, setNamespace] = useState(base?.namespace);
  const namespace = chosenNamespace ?? fallbackNamespace;
  const [name, setName] = useState(agent?.name ?? (copyOf ? copyName(copyOf.name) : ""));

  const [templateSource, setTemplateSource] = useState<Source>(spec?.template.inline ? "inline" : "reference");
  const [templateRef, setTemplateRef] = useState(spec?.template.ref?.name);
  const [templateDraft, setTemplateDraft] = useState(() =>
    base && spec?.template.inline ? draftFromSpec(spec.template.inline, base.namespace) : emptyDraft(namespace),
  );

  const [chosenHarness, setHarnessRef] = useState(spec?.harnessRef.name);
  const [creatingHarness, setCreatingHarness] = useState(false);
  const [egressRefs, setEgressRefs] = useState<string[]>(
    () => (spec?.egressPolicyRefs ?? []).map((ref) => ref.name),
  );

  const templates = useAgentTemplates(namespace || undefined);
  const harnesses = useHarnesses(namespace || undefined);
  const egressPolicies = useNamespaceEgressPolicies(namespace || undefined);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  const inlineTemplate = { ...templateDraft, namespace };
  const harnessNames = (harnesses.data ?? []).map((row) => row.name);
  // A new agent starts on the obvious harness until one is chosen; an edit keeps its own.
  const harnessRef = chosenHarness ?? (agent ? undefined : preferredHarness(harnessNames));
  const adapter = harnesses.data?.find((row) => row.name === harnessRef)?.runtime;
  const modelRef = templateSource === "reference"
    ? splitRef(templates.data?.find((row) => row.name === templateRef)?.modelConfigRef)
    : inlineTemplate.modelConfig?.name.trim()
      ? { namespace: inlineTemplate.modelConfig.namespace || namespace, name: inlineTemplate.modelConfig.name.trim() }
      : undefined;
  const model = useModel(modelRef?.namespace, modelRef?.name);
  const conflict = harnessModelConflict(adapter, model.data?.spec);

  const issues: FieldIssue<AgentField>[] = [
    ...(namespace ? [] : [{ field: "namespace" as const, message: "A namespace is required." }]),
    ...(!name.trim()
      ? [{ field: "name" as const, message: "A name is required." }]
      : !agent && !RFC1123_SUBDOMAIN.test(name.trim())
        ? [{ field: "name" as const, message: "Use lowercase letters, numbers and hyphens only." }]
        : []),
    ...(!harnessRef ? [{ field: "harness" as const, message: "Choose a harness." }] : []),
    ...(templateSource === "reference" && !templateRef
      ? [{ field: "template" as const, message: "Choose an agent template." }]
      : []),
    ...(templateSource === "inline"
      ? draftProblems(inlineTemplate, { isCreate: false }).map((message) => ({ field: "template" as const, message }))
      : []),
    // The compiler's rule, not the CRD's: only a bring-your-own harness runs without a model.
    ...(templateSource === "inline" && adapter !== "byo" && !inlineTemplate.modelConfig?.name.trim()
      ? [{ field: "template" as const, message: "Choose a model configuration for the inline template." }]
      : []),
  ];
  const validation = useSubmitValidation(issues);

  // Compared by value, so defaults filled in after mount and typed-then-deleted text are not edits.
  const contents = () =>
    JSON.stringify([chosenNamespace, name, templateSource, templateRef, { ...templateDraft, namespace: "" }, chosenHarness, egressRefs]);
  const [baseline] = useState(contents);
  const [saved, setSaved] = useState(false);
  const blocker = useUnsavedChangesGuard(!saved && contents() !== baseline);

  async function save() {
    setFailure(undefined);
    if (!validation.attempt()) return;
    setSaving(true);
    const agentSpec = {
      ...(egressRefs.length > 0 ? { egressPolicyRefs: egressRefs.map((ref) => ({ name: ref })) } : {}),
      template: templateSource === "reference"
        ? { ref: { name: templateRef! } }
        : { inline: specFromDraft(inlineTemplate, spec?.template.inline) },
      harnessRef: { name: harnessRef! },
    } as AgentSpec;
    const input = {
      namespace,
      name: name.trim(),
      resource: {
        metadata: { ...agent?.resource.metadata, namespace, name: name.trim() },
        spec: agentSpec,
      },
    };
    try {
      const result = agent
        ? await apiClient.agentBuildingBlocks.updateAgent(input)
        : await apiClient.agentBuildingBlocks.createAgent(input);
      await invalidateAgents().catch(() => {});
      toastSaved(agent ? "saved" : "created", "agent", result.name);
      // Committed before navigating, so the guard lets the save's own navigation through.
      flushSync(() => setSaved(true));
      void navigate(agentsTab);
    } catch (cause: unknown) {
      setFailure(cause);
    } finally {
      setSaving(false);
    }
  }

  const templateError = validation.errorFor("template");

  return (
    <Space
      orientation="vertical"
      size="large"
      css={{
        display: "flex",
        maxWidth: 860,
        // A card's last field drops antd's 24px item margin, so every card ends on its own padding.
        "& .ant-card-body .ant-form:last-child > .ant-form-item:last-child": { marginBottom: 0 },
      }}
    >
      <Card size="small">
        <Form
          layout="vertical"
          component="div"
          css={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))",
            gap: `${theme.space(6)} ${theme.space(4)}`,
            "& > .ant-form-item": { marginBottom: 0 },
          }}
        >
          <FormField
            label="Name"
            required
            htmlFor="agent-form-name"
            error={validation.errorFor("name")}
            extra={agent ? "A Kubernetes name, so it cannot be changed." : undefined}
          >
            <Input
              id="agent-form-name"
              data-testid="agent-form-name"
              value={name}
              disabled={Boolean(agent)}
              onChange={(event) => setName(event.target.value)}
              placeholder="my-agent"
            />
          </FormField>
          <FormField
            label="Namespace"
            required
            htmlFor="agent-form-namespace"
            error={validation.errorFor("namespace")}
            extra={agent ? "Fixed once the agent exists." : undefined}
          >
            <NamespaceSelect
              id="agent-form-namespace"
              data-testid="agent-form-namespace"
              value={namespace}
              disabled={Boolean(agent)}
              onChange={(value) => {
                // Refs resolve in the agent's namespace, so a new one clears them, sub-agents included.
                setNamespace(value);
                setTemplateRef(undefined);
                setHarnessRef(undefined);
                setEgressRefs([]);
                setTemplateDraft({ ...templateDraft, modelConfig: undefined, subAgentTools: [] });
              }}
            />
          </FormField>
        </Form>
      </Card>

      <Card
        size="small"
        data-testid="agent-form-harness"
        title={
          <SectionTitle
            title="Harness"
            summary="Where the agent runs: the runtime adapter, its image and the Substrate worker pool."
          />
        }
        extra={
          <Button
            data-testid="agent-form-harness-create"
            disabled={!namespace}
            onClick={() => setCreatingHarness(true)}
          >
            New harness
          </Button>
        }
      >
        <RefSelect
          kind="harness"
          label="Harness"
          help="A harness in this namespace. Other agents can use it too."
          value={harnessRef}
          onChange={setHarnessRef}
          loading={harnesses.isLoading}
          loadError={harnesses.error?.message}
          error={validation.errorFor("harness")}
          names={harnessNames}
          empty={
            <span>
              No harnesses in this namespace.{" "}
              <Button type="link" size="small" css={{ padding: 0 }} onClick={() => setCreatingHarness(true)}>
                Create a harness
              </Button>
            </span>
          }
        />
        {conflict ? (
          <Alert
            type="warning"
            showIcon
            data-testid="agent-form-harness-conflict"
            title="This harness may not run the template's model"
            description={conflict}
          />
        ) : null}
      </Card>

      {/*
        A controlled modal rather than `Modal.confirm`: antd 6's static methods cannot
        read context, and the warning they log is a failure in the browser suite.
      */}
      <Modal
        open={creatingHarness}
        title="New harness"
        footer={null}
        width={640}
        destroyOnHidden
        onCancel={() => setCreatingHarness(false)}
        data-testid="agent-form-harness-modal"
      >
        <HarnessIntro />
        <HarnessForm
          namespace={namespace}
          inDialog
          onSaved={(created) => {
            setHarnessRef(created.name);
            setCreatingHarness(false);
          }}
          cancel={{ onClick: () => setCreatingHarness(false) }}
        />
      </Modal>

      <Card
        size="small"
        data-testid="agent-form-template"
        title={<SectionTitle title="Agent template" summary="What the agent does: its model, prompt and tools." />}
        extra={
          <Radio.Group
            optionType="button"
            aria-label="Agent template source"
            data-testid="agent-form-template-source"
            value={templateSource}
            onChange={(event) => setTemplateSource(event.target.value)}
            options={[
              { value: "reference", label: "Use existing" },
              { value: "inline", label: "Define here" },
            ]}
          />
        }
      >
        {templateSource === "reference" ? (
          <RefSelect
            kind="template"
            label="Agent template"
            help="A template in this namespace. Other agents can use it too."
            value={templateRef}
            onChange={setTemplateRef}
            loading={templates.isLoading}
            loadError={templates.error?.message}
            error={templateError}
            names={(templates.data ?? []).map((row) => row.name)}
          />
        ) : (
          <>
            <AgentTemplateForm
              embedded
              draft={inlineTemplate}
              onChange={setTemplateDraft}
              isCreate={false}
              namespace={namespace}
              hasUnshownFields={spec?.template.inline ? hasUnshownSpecFields(spec.template.inline) : false}
            />
            {templateError ? (
              <Text
                role="alert"
                data-testid="agent-form-template-error"
                css={{ display: "block", marginTop: theme.space(2), color: theme.color.dangerText }}
              >
                {templateError}
              </Text>
            ) : null}
          </>
        )}
      </Card>

      <Card
        size="small"
        data-testid="agent-form-egress"
        title={<SectionTitle title={egressCopy.agent.field} summary={egressCopy.agent.help} />}
      >
        <Form layout="vertical" component="div">
          <FormField
            label={egressCopy.agent.field}
            required={false}
            htmlFor="agent-form-egress-refs"
            error={egressPolicies.error?.message}
          >
            <Select
              id="agent-form-egress-refs"
              mode="multiple"
              allowClear
              data-testid="agent-form-egress-refs"
              placeholder={egressCopy.agent.placeholder}
              value={egressRefs}
              onChange={setEgressRefs}
              loading={egressPolicies.isLoading}
              maxCount={16}
              options={(egressPolicies.data ?? []).map((policy) => ({ value: policy.name, label: policy.name }))}
              notFoundContent={egressPolicies.isLoading ? undefined : egressCopy.agent.empty}
            />
          </FormField>
        </Form>
      </Card>

      <FormActions
        submitLabel={agent ? "Save" : "Create"}
        onSubmit={() => void save()}
        submitting={saving}
        cancel={{ to: agentsTab }}
        testIdPrefix="agent-form"
        error={
          failure !== undefined ? (
            <SubmitError
              error={failure}
              what="agent"
              outcome={agent ? "saved" : "created"}
              onRetry={() => void save()}
              data-testid="agent-form-error"
            />
          ) : validation.issues.length > 0 ? <ValidationNotice data-testid="agent-form-error" /> : null
        }
      />

      <UnsavedChangesModal blocker={blocker} testIdPrefix="agent-form">
        Leaving now discards your edits. Nothing changes on the cluster.
      </UnsavedChangesModal>
    </Space>
  );
}

/** `namespace/name` as its parts; undefined for an empty ref. */
function splitRef(ref: string | undefined): { namespace: string; name: string } | undefined {
  const [namespace, name] = ref?.split("/") ?? [];
  return namespace && name ? { namespace, name } : undefined;
}

function SectionTitle({ title, summary }: { title: string; summary: string }) {
  const theme = useTheme();
  return (
    <div css={{ padding: `${theme.space(2)} 0` }}>
      <div>{title}</div>
      <Text css={{ color: theme.color.textMuted, fontSize: 12, fontWeight: "normal", whiteSpace: "normal" }}>
        {summary}
      </Text>
    </div>
  );
}

function RefSelect({
  kind,
  label,
  help,
  value,
  onChange,
  loading,
  loadError,
  error,
  names,
  empty,
}: {
  kind: string;
  label: string;
  help: string;
  value?: string;
  onChange: (next: string) => void;
  loading: boolean;
  /** A failed read of the list. */
  loadError?: string;
  /** A submit-time problem with the choice. */
  error?: string;
  names: string[];
  empty?: ReactNode;
}) {
  return (
    <Form layout="vertical" component="div">
      <FormField
        label={label}
        required
        htmlFor={`agent-form-${kind}-ref`}
        error={error ?? loadError}
        extra={help}
      >
        <ResourceSelect
          kind={kind}
          id={`agent-form-${kind}-ref`}
          data-testid={`agent-form-${kind}-ref`}
          value={value}
          onChange={onChange}
          loading={loading}
          error={loadError}
          names={names}
          empty={empty ?? `No ${kind}s in this namespace`}
        />
      </FormField>
    </Form>
  );
}
