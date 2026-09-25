import { useMemo, useState, type ReactNode } from "react";
import { Alert, Button, Card, Form, Input, Radio, Select, Skeleton, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate, useParams } from "react-router-dom";
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
import { HarnessFields } from "@/components/harness-form/HarnessFields";
import {
  emptyHarnessDraft,
  harnessDraftFromSpec,
  harnessDraftProblems,
  harnessSpecFromDraft,
} from "@/components/harness-form/harnessDraft";
import {
  apiClient,
  useAgent,
  useAgentTemplates,
  useHarnesses,
  useInvalidateAgents,
  useNamespaces,
  type Agent,
  type AgentSpec,
} from "@/api";
import { paths } from "@/router/routes";

const { Text } = Typography;

type Source = "reference" | "inline";

/** Create or edit an Agent. Edit is the `agentEdit` route, whose name and namespace are fixed. */
export function AgentFormPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const editing = Boolean(namespace && name);
  const existing = useAgent(namespace, name);
  const namespaces = useNamespaces();

  return (
    <PageFrame
      title={editing ? `Edit agent ${name}` : "New agent"}
      description="An agent pairs a template with a harness. Reference a shared one, or define it inline for this agent only."
    >
      {!editing && namespaces.error ? (
        <Alert
          type="error"
          showIcon
          title="Could not load namespaces"
          description={namespaces.error.message}
          data-testid="agent-form-load-error"
        />
      ) : !editing && namespaces.data ? (
        // Mounted once namespaces arrive, so the ref lists never read an empty namespace.
        <AgentForm />
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

function AgentForm({ agent }: { agent?: Agent }) {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const invalidateAgents = useInvalidateAgents();
  const spec = agent?.resource.spec;

  const fallbackNamespace = useMemo(() => {
    const names = (namespaces.data ?? []).map((entry) => entry.name);
    return names.includes("kagent") ? "kagent" : (names[0] ?? "");
  }, [namespaces.data]);

  const [chosenNamespace, setNamespace] = useState(agent?.namespace);
  const namespace = chosenNamespace ?? fallbackNamespace;
  const [name, setName] = useState(agent?.name ?? "");

  const [templateSource, setTemplateSource] = useState<Source>(spec?.template ? "inline" : "reference");
  const [templateRef, setTemplateRef] = useState(spec?.templateRef?.name);
  const [templateDraft, setTemplateDraft] = useState(() =>
    spec?.template ? draftFromSpec(spec.template, agent!.namespace) : emptyDraft(namespace),
  );

  const [harnessSource, setHarnessSource] = useState<Source>(spec?.harness ? "inline" : "reference");
  const [harnessRef, setHarnessRef] = useState(spec?.harnessRef?.name);
  const [harnessDraft, setHarnessDraft] = useState(() =>
    spec?.harness ? harnessDraftFromSpec(spec.harness) : emptyHarnessDraft(),
  );

  const templates = useAgentTemplates(namespace || undefined);
  const harnesses = useHarnesses(namespace || undefined);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string>();

  const inlineTemplate = { ...templateDraft, namespace };
  const adapter = harnessSource === "inline"
    ? harnessDraft.adapter
    : harnesses.data?.find((row) => row.name === harnessRef)?.runtime;
  const problems = [
    ...(namespace ? [] : ["A namespace is required."]),
    ...(name.trim() ? [] : ["A name is required."]),
    ...(templateSource === "reference" && !templateRef ? ["Choose a template."] : []),
    ...(templateSource === "inline" ? draftProblems(inlineTemplate, { isCreate: false }) : []),
    // The compiler's rule, not the CRD's: only a bring-your-own harness runs without a model.
    ...(templateSource === "inline" && adapter !== "byo" && !inlineTemplate.modelConfig.trim()
      ? ["Choose a model configuration for the inline template."]
      : []),
    ...(harnessSource === "reference" && !harnessRef ? ["Choose a harness."] : []),
    ...(harnessSource === "inline" ? harnessDraftProblems(harnessDraft) : []),
  ];

  async function save() {
    setSaving(true);
    setFailure(undefined);
    const agentSpec = {
      ...(templateSource === "reference"
        ? { templateRef: { name: templateRef! } }
        : { template: specFromDraft(inlineTemplate, spec?.template) }),
      ...(harnessSource === "reference"
        ? { harnessRef: { name: harnessRef! } }
        : { harness: harnessSpecFromDraft(harnessDraft, spec?.harness) }),
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
                setTemplateDraft({ ...templateDraft, modelConfig: "", agentTools: [] });
              }}
              options={(namespaces.data ?? []).map((entry) => ({ value: entry.name, label: entry.name }))}
            />
          </Form.Item>
          <Form.Item label="Name" required extra={agent ? "A Kubernetes name, so it cannot be changed." : undefined}>
            <Input
              data-testid="agent-form-name"
              value={name}
              disabled={Boolean(agent)}
              onChange={(event) => setName(event.target.value)}
              placeholder="my-agent"
            />
          </Form.Item>
        </Form>
      </Card>

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
            hasUnshownFields={spec?.template ? hasUnshownSpecFields(spec.template) : false}
          />
        }
      />

      <SourceSection
        kind="harness"
        title="Harness"
        summary="How and where the agent runs: its runtime, image and worker pool."
        source={harnessSource}
        onSource={setHarnessSource}
        reference={
          <RefSelect
            kind="harness"
            value={harnessRef}
            onChange={setHarnessRef}
            loading={harnesses.isLoading}
            error={harnesses.error?.message}
            names={(harnesses.data ?? []).map((row) => row.name)}
          />
        }
        inline={
          <Form layout="vertical">
            <HarnessFields draft={harnessDraft} onChange={setHarnessDraft} />
          </Form>
        }
      />

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
          <Text data-testid="agent-form-problems" css={{ color: theme.color.textMuted, fontSize: 12 }}>
            {problems[0]}
          </Text>
        ) : null}
      </div>
    </Space>
  );
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
}: {
  kind: string;
  value?: string;
  onChange: (next: string) => void;
  loading: boolean;
  error?: string;
  names: string[];
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
          notFoundContent={loading ? undefined : `No ${kind}s in this namespace`}
        />
      </Form.Item>
    </Form>
  );
}
