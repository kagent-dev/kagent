import { useMemo, useState, type ReactNode } from "react";
import { Alert, Button, Card, Form, Input, Modal, Radio, Select, Skeleton, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import toast from "react-hot-toast";
import { PageFrame } from "@/components/Structure/PageFrame";
import { AgentTemplateForm } from "@/components/agent-template-form/AgentTemplateForm";
import {
  draftFromSpec,
  draftProblems,
  emptyDraft,
  specFromDraft,
} from "@/components/agent-template-form/agentTemplateDraft";
import { hasUnshownSpecFields } from "@/components/agent-template-form/unshownFields";
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
import { copySource } from "@/components/table/RowActionsMenu";
import { paths } from "@/router/routes";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import { HarnessForm } from "@/components/harness-form/HarnessForm";
import { HarnessIntro } from "@/components/harness-form/HarnessFields";
import { harnessModelConflict, preferredHarness } from "@/components/harness-form/harnessChoice";

const { Text } = Typography;

type Source = "reference" | "inline";

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

  return (
    <PageFrame
      title={editing ? `Edit agent ${name}` : "New agent"}
      description="An agent pairs inline or shared behavior with a platform-managed harness."
    >
      {!editing && namespaces.error ? (
        <Alert
          type="error"
          showIcon
          title="Could not load namespaces"
          description={namespaces.error.message}
          data-testid="agent-form-load-error"
        />
      ) : !editing && namespaces.data && (!copying || source.data || source.error) ? (
        // Mounted once namespaces arrive, so the ref lists never read an empty namespace.
        // A copy source that could not be had leaves a blank form, with the reason above it.
        <>
          {copying && source.error && !source.data ? (
            <Alert
              type="error"
              showIcon
              title="Could not load the agent to copy"
              description={`${source.error.message} Starting from a blank form.`}
              data-testid="agent-copy-error"
              css={{ maxWidth: 860, marginBottom: 16 }}
            />
          ) : null}
          <AgentForm
            key={source.data ? `${source.data.namespace}/${source.data.name}` : "new"}
            copyOf={source.data}
          />
        </>
      ) : editing && existing.error ? (
        <Alert
          type="error"
          showIcon
          title="Could not load this agent"
          description={existing.error.message}
          data-testid="agent-form-load-error"
        />
      ) : editing && existing.data ? (
        <AgentForm agent={existing.data} />
      ) : (
        <Card css={{ maxWidth: 860 }}>
          <Skeleton active paragraph={{ rows: 8 }} />
        </Card>
      )}
    </PageFrame>
  );
}

function AgentForm({ agent, copyOf }: { agent?: Agent; copyOf?: Agent }) {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const invalidateAgents = useInvalidateAgents();
  const spec = (agent ?? copyOf)?.resource.spec;

  const fallbackNamespace = useMemo(() => {
    const names = (namespaces.data ?? []).map((entry) => entry.name);
    return names.includes("kagent") ? "kagent" : (names[0] ?? "");
  }, [namespaces.data]);

  const [chosenNamespace, setNamespace] = useState((agent ?? copyOf)?.namespace);
  const namespace = chosenNamespace ?? fallbackNamespace;
  const [name, setName] = useState(agent?.name ?? (copyOf ? `${copyOf.name}-copy` : ""));

  const [templateSource, setTemplateSource] = useState<Source>(spec?.template.inline ? "inline" : "reference");
  const [templateRef, setTemplateRef] = useState(spec?.template.ref?.name);
  const [templateDraft, setTemplateDraft] = useState(() =>
    spec?.template.inline ? draftFromSpec(spec.template.inline, (agent ?? copyOf)!.namespace) : emptyDraft(namespace),
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
  const [failure, setFailure] = useState<string>();

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
  const problems = [
    ...(namespace ? [] : ["A namespace is required."]),
    ...(name.trim() ? [] : ["A name is required."]),
    ...(templateSource === "reference" && !templateRef ? ["Choose a template."] : []),
    ...(templateSource === "inline" ? draftProblems(inlineTemplate, { isCreate: false }) : []),
    // The compiler's rule, not the CRD's: only a bring-your-own harness runs without a model.
    ...(templateSource === "inline" && adapter !== "byo" && !inlineTemplate.modelConfig?.name.trim()
      ? ["Choose a model configuration for the inline template."]
      : []),
    ...(!harnessRef ? ["Choose a harness."] : []),
  ];

  async function save() {
    setSaving(true);
    setFailure(undefined);
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
      const saved = agent
        ? await apiClient.agentBuildingBlocks.updateAgent(input)
        : await apiClient.agentBuildingBlocks.createAgent(input);
      await invalidateAgents().catch(() => {});
      toast.success(`Agent ${saved.name} ${agent ? "saved" : "created"}`);
      navigate(`${paths.agents}?tab=agents`);
    } catch (cause: unknown) {
      setFailure(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Space orientation="vertical" size="large" css={{ display: "flex", maxWidth: 860 }}>
      <Card size="small">
        <Form layout="vertical" css={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", columnGap: theme.space(4) }}>
          <Form.Item label="Name" required extra={agent ? "A Kubernetes name, so it cannot be changed." : undefined}>
            <Input
              data-testid="agent-form-name"
              value={name}
              disabled={Boolean(agent)}
              onChange={(event) => setName(event.target.value)}
              placeholder="my-agent"
            />
          </Form.Item>
          <Form.Item label="Namespace" required extra={agent ? "Fixed once the agent exists." : undefined}>
            <Select
              data-testid="agent-form-namespace"
              value={namespace || undefined}
              disabled={Boolean(agent)}
              loading={namespaces.isLoading}
              onChange={(value: string) => {
                // Refs resolve in the agent's namespace, so a new one clears them, sub-agents included.
                setNamespace(value);
                setTemplateRef(undefined);
                setHarnessRef(undefined);
                setEgressRefs([]);
                setTemplateDraft({ ...templateDraft, modelConfig: undefined, subAgentTools: [] });
              }}
              options={(namespaces.data ?? []).map((entry) => ({ value: entry.name, label: entry.name }))}
            />
          </Form.Item>
        </Form>
      </Card>

      <Card
        size="small"
        data-testid="agent-form-harness"
        title={
          <div css={{ padding: `${theme.space(2)} 0` }}>
            <div>Harness</div>
            <Text css={{ color: theme.color.textMuted, fontSize: 12, fontWeight: "normal", whiteSpace: "normal" }}>
              Where the agent runs: the runtime adapter, its image and the Substrate worker pool.
            </Text>
          </div>
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
          value={harnessRef}
          onChange={setHarnessRef}
          loading={harnesses.isLoading}
          error={harnesses.error?.message}
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
          onSaved={(saved) => {
            setHarnessRef(saved.name);
            setCreatingHarness(false);
            toast.success(`Harness ${saved.name} created`);
          }}
          onCancel={() => setCreatingHarness(false)}
        />
      </Modal>

      <SourceSection
        kind="template"
        title="Template"
        summary="What the agent does: its model, prompt and tools."
        source={templateSource}
        onSource={setTemplateSource}
        reference={
          <RefSelect
            kind="template"
            value={templateRef}
            onChange={setTemplateRef}
            loading={templates.isLoading}
            error={templates.error?.message}
            names={(templates.data ?? []).map((row) => row.name)}
          />
        }
        inline={
          <AgentTemplateForm
            embedded
            draft={inlineTemplate}
            onChange={setTemplateDraft}
            isCreate={false}
            namespace={namespace}
            hasUnshownFields={spec?.template.inline ? hasUnshownSpecFields(spec.template.inline) : false}
          />
        }
      />

      <Card
        size="small"
        data-testid="agent-form-egress"
        title={
          <div css={{ padding: `${theme.space(2)} 0` }}>
            <div>{egressCopy.agent.field}</div>
            <Text css={{ color: theme.color.textMuted, fontSize: 12, fontWeight: "normal", whiteSpace: "normal" }}>
              {egressCopy.agent.help}
            </Text>
          </div>
        }
      >
        <Form layout="vertical">
          <Form.Item
            label={egressCopy.agent.field}
            htmlFor="agent-form-egress-refs"
            validateStatus={egressPolicies.error ? "error" : undefined}
            help={egressPolicies.error?.message}
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
          </Form.Item>
        </Form>
      </Card>

      {failure ? (
        <Alert
          type="error"
          showIcon
          data-testid="agent-form-error"
          title={agent ? "Could not save this agent" : "Could not create this agent"}
          description={failure}
        />
      ) : null}

      <div
        css={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: theme.space(2),
          paddingTop: theme.space(5),
          borderTop: `1px solid ${theme.color.border}`,
        }}
      >
        <Button
          type="primary"
          data-testid="agent-form-submit"
          loading={saving}
          disabled={problems.length > 0}
          onClick={() => void save()}
        >
          {agent ? "Save agent" : "Create agent"}
        </Button>
        <Button onClick={() => navigate(`${paths.agents}?tab=agents`)}>Cancel</Button>
        {problems.length > 0 ? (
          <ul
            data-testid="agent-form-problems"
            css={{ color: theme.color.textMuted, fontSize: 12, margin: 0, paddingLeft: theme.space(4) }}
          >
            {problems.map((problem) => (
              <li key={problem}>{problem}</li>
            ))}
          </ul>
        ) : null}
      </div>
    </Space>
  );
}

/** `namespace/name` as its parts; undefined for an empty ref. */
function splitRef(ref: string | undefined): { namespace: string; name: string } | undefined {
  const [namespace, name] = ref?.split("/") ?? [];
  return namespace && name ? { namespace, name } : undefined;
}

function SourceSection({
  kind,
  title,
  summary,
  source,
  onSource,
  reference,
  inline,
}: {
  kind: string;
  title: string;
  summary: string;
  source: Source;
  onSource: (next: Source) => void;
  reference: ReactNode;
  inline: ReactNode;
}) {
  const theme = useTheme();
  return (
    <Card
      size="small"
      data-testid={`agent-form-${kind}`}
      title={
        <div css={{ padding: `${theme.space(2)} 0` }}>
          <div>{title}</div>
          <Text css={{ color: theme.color.textMuted, fontSize: 12, fontWeight: "normal", whiteSpace: "normal" }}>
            {summary}
          </Text>
        </div>
      }
      extra={
        <Radio.Group
          optionType="button"
          aria-label={`${title} source`}
          data-testid={`agent-form-${kind}-source`}
          value={source}
          onChange={(event) => onSource(event.target.value)}
          options={[
            { value: "reference", label: "Reference" },
            { value: "inline", label: "Inline" },
          ]}
        />
      }
    >
      {source === "reference" ? reference : inline}
    </Card>
  );
}

function RefSelect({
  kind,
  value,
  onChange,
  loading,
  error,
  names,
  empty,
}: {
  kind: string;
  value?: string;
  onChange: (next: string) => void;
  loading: boolean;
  error?: string;
  names: string[];
  empty?: ReactNode;
}) {
  return (
    <Form layout="vertical">
      <Form.Item
        label={`Shared ${kind}`}
        required
        validateStatus={error ? "error" : undefined}
        help={error ?? `A ${kind} in this namespace. Other agents can use it too.`}
      >
        <Select
          data-testid={`agent-form-${kind}-ref`}
          placeholder={`Choose a ${kind}`}
          value={value}
          onChange={onChange}
          loading={loading}
          showSearch
          options={names.map((name) => ({ value: name, label: name }))}
          notFoundContent={loading ? undefined : (empty ?? `No ${kind}s in this namespace`)}
        />
      </Form.Item>
    </Form>
  );
}
