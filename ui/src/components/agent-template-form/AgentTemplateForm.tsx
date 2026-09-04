import { useEffect, useMemo, useRef } from "react";
import {
  Alert,
  Button,
  Form,
  Input,
  Select,
  Space,
  Tag,
  Typography,
} from "antd";
import { useTheme } from "@emotion/react";
import { Plus, Trash } from "lucide-react";
import {
  admitsLabels,
  harnessSelector,
  useHarnesses,
  useMcpServers,
  useModels,
  useTools,
  type Harness,
} from "@/api";
import {
  draftProblems,
  type AgentTemplateDraft,
} from "./agentTemplateDraft";

const { Text, Paragraph } = Typography;

/**
 * The agent-template form, shared by every place one can be authored.
 *
 * One component rather than two, because the alternative is two forms that agree
 * today: the templates page and the inline panel on the agent-create page both
 * write the same CRD, and a field added to one of them silently missing from the
 * other is the kind of drift nobody notices until a template made one way is
 * missing something a template made the other way has.
 *
 * It is deliberately presentational — it holds no draft of its own and performs no
 * write. The page above owns the draft, decides what a save means (create, update,
 * or create-and-select) and reports what the controller said. That is what lets the
 * same component sit on a page and inside a modal without either knowing about the
 * other.
 *
 * ## Why reading is a mode of this form and not a second component
 *
 * The template details page shows a template read-only before offering to edit it.
 * Building that as its own view would give one spec two renderings, and two
 * renderings of one spec drift: the one nobody edits is the one that quietly stops
 * showing a field the CRD gained. So `readOnly` turns the same fields into text
 * rather than replacing them.
 *
 * What it changes is only what it has to. Inputs become borderless and read-only,
 * selects stop opening, and every control that *authors* rather than displays — the
 * add and remove buttons, the "make it run on" buttons — is gone, because there is
 * nothing they could do. Placeholders go too, and that is the detail worth naming: a
 * greyed placeholder in an empty borderless input is indistinguishable from a value,
 * so a template with no system prompt would appear to have the example one.
 *
 * ## What it does not author, and why that is safe
 *
 * `skills`, `plugins` and `promptTemplate` are not on this form. Each is a rich
 * shape of its own — three artifact-source variants with strict CEL patterns — and
 * authoring them properly is its own piece of work. What matters is that an edit
 * does not *lose* them: `specFromDraft` merges onto the spec it was given rather
 * than building a fresh one, and `agentTemplateDraft.test.ts` pins that.
 *
 * The form says so on screen too, when the template it is editing has them. A
 * reader who cannot see a field they know is set should be told it is still there
 * rather than left to assume it was dropped.
 */
export function AgentTemplateForm({
  draft,
  onChange,
  isCreate,
  namespace,
  hasUnshownFields,
  readOnly = false,
}: {
  draft: AgentTemplateDraft;
  onChange: (next: AgentTemplateDraft) => void;
  /** Create shows the name field; an edit cannot rename a Kubernetes object. */
  isCreate: boolean;
  namespace: string;
  /** Whether the template being edited carries spec fields this form does not show. */
  hasUnshownFields?: boolean;
  /**
   * Render the same fields as text.
   *
   * The details page's Details tab, before its Edit button is pressed. See this
   * component's note on why this is a mode rather than a second view.
   */
  readOnly?: boolean;
}) {
  const theme = useTheme();
  const models = useModels();
  const servers = useMcpServers();
  const tools = useTools();
  const harnesses = useHarnesses(namespace);

  const set = <K extends keyof AgentTemplateDraft>(
    field: K,
    value: AgentTemplateDraft[K],
  ) => onChange({ ...draft, [field]: value });

  const problems = draftProblems(draft, { isCreate });

  /**
   * An example, or the absence of a value said outright.
   *
   * A placeholder is a prompt to type something, and there is nothing to type here.
   * Left in place it renders as grey text inside an empty borderless input, which is
   * exactly what a *set* value looks like — so a template with no system prompt would
   * read as having the example one.
   */
  const placeholder = (example: string) => (readOnly ? "Not set" : example);

  /** The props that turn a text input into a line of text. */
  const readOnlyInput = readOnly
    ? ({ readOnly: true, variant: "borderless" } as const)
    : {};

  /** The props that turn a select into a line of text. It still shows its value. */
  const readOnlySelect = readOnly
    ? ({ open: false, variant: "borderless", suffixIcon: null } as const)
    : {};

  /** The tools each server exposes, so the tool picker offers real names. */
  const toolsByServer = useMemo(() => {
    const grouped = new Map<string, string[]>();
    for (const tool of tools.data ?? []) {
      const list = grouped.get(tool.server_name) ?? [];
      list.push(tool.id);
      grouped.set(tool.server_name, list);
    }
    return grouped;
  }, [tools.data]);

  /*
   * Which harnesses would admit this template, given the labels as they stand.
   *
   * Computed here rather than read from the resource because the resource's own
   * answer is the controller's, and it is a generation behind whatever the reader
   * has just typed. This is the preview; the row on the list is the truth.
   *
   * Which is also why the preview is not rendered when nothing is being edited: with
   * no unsaved labels to be ahead of, it would be a second answer to a question the
   * page above has already answered from the controller — and two answers to "will
   * anything run this" that can disagree is worse than one.
   */
  const wouldAdmit = useMemo(() => {
    const labels = Object.fromEntries(
      draft.labels
        .filter((label) => label.key.trim() !== "")
        .map((label) => [label.key.trim(), label.value.trim()]),
    );
    return (harnesses.data ?? []).filter((harness) => admitsLabels(harness, labels));
  }, [draft.labels, harnesses.data]);

  /*
   * With one harness on the cluster, a new template is labelled for it without being
   * asked.
   *
   * A template no harness admits is the commonest way to end up with something that
   * looks created and can never be used, and on a single-harness cluster there is no
   * decision to make — the reader would press the one button under "Make it run on"
   * every time. So it is pressed for them, and the alert below then says which harness
   * will run it, which is where they find out it happened.
   *
   * Once, and only for a new template with no labels of its own: an edit that removed
   * the label deliberately must not have it put back, and a reader who typed their own
   * must not have it overwritten. `useEffect` rather than derivation because the
   * harnesses arrive asynchronously and this changes the draft the reader will submit —
   * a value they can then see and undo, rather than one applied invisibly at save.
   */
  const defaultedLabels = useRef(false);
  useEffect(() => {
    if (!isCreate || readOnly || defaultedLabels.current) return;
    const only = harnesses.data?.length === 1 ? harnesses.data[0] : undefined;
    if (!only || Object.keys(harnessSelector(only)).length === 0) return;
    if (draft.labels.length > 0) return;
    defaultedLabels.current = true;
    makeAdmittedBy(only);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [harnesses.data, isCreate, readOnly]);

  /** Adds whatever labels a harness selects on, so the template becomes usable by it. */
  function makeAdmittedBy(harness: Harness) {
    const selector = harnessSelector(harness);
    const next = draft.labels.filter(
      (label) => !(label.key.trim() in selector),
    );
    for (const [key, value] of Object.entries(selector)) {
      next.push({ key, value });
    }
    set("labels", next);
  }

  /** "None", said in the place a list would have been. */
  const none = (what: string) => (
    <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{what}</Text>
  );

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
      {/* What this thing is, before any field. An AgentTemplate is half of an
          agent, and a reader who does not know that cannot tell why the form has
          no "run it" button. */}
      <Alert
        type="info"
        showIcon
        data-testid="template-form-explainer"
        title="An agent template is what an agent does — not where it runs"
        description="It carries the model, the prompt and the tools. A harness carries the runtime: the adapter, the worker pool and the image. An agent is one of each, and creating an agent is choosing a pair."
      />

      <Form layout="vertical">
        {isCreate ? (
          <Form.Item
            label="Name"
            /* Unconditional: this field only exists while creating, and the only
               caller that creates does not render read-only, so `!readOnly` was a
               condition that could not be false. The model configuration below is the
               genuinely conditional one. */
            required
            extra="A Kubernetes object name, so it cannot be changed afterwards."
          >
            <Input
              data-testid="template-form-name"
              value={draft.name}
              onChange={(event) => set("name", event.target.value)}
              placeholder={placeholder("incident-responder")}
              {...readOnlyInput}
            />
          </Form.Item>
        ) : null}

        <Form.Item
          label="Model configuration"
          /* Marked required only while the form authors: read-only is the details page
             showing a template that already has a model, and an asterisk there would be
             asking a reader for something the template has. */
          required={!readOnly}
          extra="The only field the CRD requires. It names a ModelConfig in this template's own namespace."
        >
          <div data-testid="template-form-model">
            <Select
              css={{ minWidth: 320 }}
              value={draft.modelConfig || undefined}
              loading={models.isLoading}
              placeholder={placeholder("Choose a model configuration")}
              popupMatchSelectWidth={false}
              onChange={(value: string) => set("modelConfig", value)}
              options={(models.data ?? [])
                .filter((model) => model.ref.startsWith(`${namespace}/`))
                .map((model) => {
                  const name = model.ref.slice(namespace.length + 1);
                  return {
                    value: name,
                    // `title` carries the label verbatim, which is what a spec
                    // locates an option by — `getByRole("option")` matches
                    // rc-select's hidden screen-reader listbox instead.
                    title: name,
                    label: (
                      <Space size={8}>
                        <span>{name}</span>
                        <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                          {model.spec.model}
                        </Text>
                      </Space>
                    ),
                  };
                })}
              {...readOnlySelect}
            />
          </div>
        </Form.Item>

        <Form.Item label="Description">
          <Input
            data-testid="template-form-description"
            value={draft.description}
            onChange={(event) => set("description", event.target.value)}
            placeholder={placeholder("What this agent is for.")}
            {...readOnlyInput}
          />
        </Form.Item>

        <Form.Item
          label="System prompt"
          extra="Inline, or read from a ConfigMap. The CRD rejects a template that has both."
        >
          <Space orientation="vertical" size={8} css={{ display: "flex" }}>
            <div data-testid="template-form-prompt-source">
              <Select
                css={{ minWidth: 240 }}
                value={draft.promptSource}
                onChange={(value: "inline" | "configMap") => set("promptSource", value)}
                options={[
                  { value: "inline", title: "Written here", label: "Written here" },
                  {
                    value: "configMap",
                    title: "Read from a ConfigMap",
                    label: "Read from a ConfigMap",
                  },
                ]}
                {...readOnlySelect}
              />
            </div>

            {draft.promptSource === "inline" ? (
              <Input.TextArea
                data-testid="template-form-prompt"
                value={draft.systemPrompt}
                onChange={(event) => set("systemPrompt", event.target.value)}
                // Three rows is room to write in. Reading, it is three rows of blank
                // under one line of prompt, which looks like a field that lost its value.
                autoSize={{ minRows: readOnly ? 1 : 3, maxRows: 12 }}
                placeholder={placeholder(
                  "You are a Kubernetes operations assistant. Never guess.",
                )}
                {...readOnlyInput}
              />
            ) : (
              <Space size={8}>
                <Input
                  data-testid="template-form-prompt-configmap"
                  value={draft.systemPromptConfigMap}
                  onChange={(event) => set("systemPromptConfigMap", event.target.value)}
                  placeholder={placeholder("ConfigMap name")}
                  {...readOnlyInput}
                />
                <Input
                  data-testid="template-form-prompt-key"
                  value={draft.systemPromptKey}
                  onChange={(event) => set("systemPromptKey", event.target.value)}
                  placeholder={placeholder("Key")}
                  {...readOnlyInput}
                />
              </Space>
            )}
          </Space>
        </Form.Item>

        {/* Tools — MCP servers */}
        <Form.Item
          label="Tools from MCP servers"
          extra="Each binding names one server and optionally limits which tools to expose. An empty selection exposes every tool from that server."
        >
          <Space orientation="vertical" size={8} css={{ display: "flex" }}>
            {readOnly && draft.mcpTools.length === 0
              ? none("No MCP tools.")
              : null}

            {draft.mcpTools.map((tool, index) => (
              <Space key={index} size={8} align="start" data-testid={`template-form-mcp-${index}`}>
                <Select
                  css={{ minWidth: 220 }}
                  value={tool.serverRef || undefined}
                  loading={servers.isLoading}
                  placeholder={placeholder("MCP server")}
                  popupMatchSelectWidth={false}
                  onChange={(value: string) => {
                    const next = [...draft.mcpTools];
                    // The tools belong to the server, so changing it clears them
                    // rather than leaving names the new server does not have.
                    next[index] = { serverRef: value, tools: [] };
                    set("mcpTools", next);
                  }}
                  options={(servers.data ?? []).map((server) => ({
                    value: server.ref,
                    title: server.ref,
                    label: server.ref,
                  }))}
                  {...readOnlySelect}
                />
                <Select
                  mode="multiple"
                  css={{ minWidth: 320 }}
                  /*
                   * Each selected tool renders as a tag with a cross on it, offering a
                   * removal that cannot happen here. Neither `removeIcon={null}` nor a
                   * `display: none` override takes it away — antd's own rule for it is
                   * more specific than a class on the select's root — so the tag is
                   * rendered outright instead, which is a smaller lie to no one.
                   */
                  {...(readOnly
                    ? {
                        tagRender: ({ label }: { label: React.ReactNode }) => (
                          <Tag>{label}</Tag>
                        ),
                      }
                    : {})}
                  value={tool.tools}
                  loading={tools.isLoading}
                  placeholder={placeholder("All tools")}
                  popupMatchSelectWidth={false}
                  onChange={(value: string[]) => {
                    const next = [...draft.mcpTools];
                    next[index] = { ...next[index], tools: value };
                    set("mcpTools", next);
                  }}
                  options={(toolsByServer.get(tool.serverRef) ?? []).map((name) => ({
                    value: name,
                    title: name,
                    label: name,
                  }))}
                  {...readOnlySelect}
                />
                {readOnly ? null : (
                  <Button
                    type="text"
                    aria-label={`Remove MCP tool binding ${index + 1}`}
                    data-testid={`template-form-mcp-remove-${index}`}
                    icon={<Trash size={14} />}
                    onClick={() =>
                      set(
                        "mcpTools",
                        draft.mcpTools.filter((_, at) => at !== index),
                      )
                    }
                  />
                )}
              </Space>
            ))}
            {readOnly ? null : (
              <Button
                size="small"
                icon={<Plus size={13} />}
                data-testid="template-form-add-mcp"
                onClick={() =>
                  set("mcpTools", [...draft.mcpTools, { serverRef: "", tools: [] }])
                }
              >
                Add an MCP server
              </Button>
            )}
          </Space>
        </Form.Item>

        {/* Tools — sub-agents */}
        <Form.Item
          label="Tools from other agent templates"
          extra="Exposes another template in this namespace as a tool this one can route work to. The description is what tells the parent when to use it, so the CRD requires it."
        >
          <Space orientation="vertical" size={8} css={{ display: "flex" }}>
            {readOnly && draft.agentTools.length === 0
              ? none("No sub-agents.")
              : null}

            {draft.agentTools.map((tool, index) => (
              <Space key={index} size={8} align="start" data-testid={`template-form-agent-${index}`}>
                <Input
                  css={{ width: 160 }}
                  value={tool.name}
                  placeholder={placeholder("Tool name")}
                  onChange={(event) => {
                    const next = [...draft.agentTools];
                    next[index] = { ...next[index], name: event.target.value };
                    set("agentTools", next);
                  }}
                  {...readOnlyInput}
                />
                <Input
                  css={{ width: 260 }}
                  value={tool.description}
                  placeholder={placeholder("When to use it")}
                  onChange={(event) => {
                    const next = [...draft.agentTools];
                    next[index] = { ...next[index], description: event.target.value };
                    set("agentTools", next);
                  }}
                  {...readOnlyInput}
                />
                <Input
                  css={{ width: 180 }}
                  value={tool.templateName}
                  placeholder={placeholder("Template name")}
                  onChange={(event) => {
                    const next = [...draft.agentTools];
                    next[index] = { ...next[index], templateName: event.target.value };
                    set("agentTools", next);
                  }}
                  {...readOnlyInput}
                />
                <Select
                  css={{ width: 130 }}
                  value={tool.isolation}
                  onChange={(value: "Shared" | "Dedicated") => {
                    const next = [...draft.agentTools];
                    next[index] = { ...next[index], isolation: value };
                    set("agentTools", next);
                  }}
                  options={[
                    { value: "Shared", title: "Shared", label: "Shared" },
                    { value: "Dedicated", title: "Dedicated", label: "Dedicated" },
                  ]}
                  {...readOnlySelect}
                />
                {readOnly ? null : (
                  <Button
                    type="text"
                    aria-label={`Remove sub-agent tool ${index + 1}`}
                    icon={<Trash size={14} />}
                    onClick={() =>
                      set(
                        "agentTools",
                        draft.agentTools.filter((_, at) => at !== index),
                      )
                    }
                  />
                )}
              </Space>
            ))}
            {readOnly ? null : (
              <Button
                size="small"
                icon={<Plus size={13} />}
                data-testid="template-form-add-agent-tool"
                onClick={() =>
                  set("agentTools", [
                    ...draft.agentTools,
                    { name: "", description: "", templateName: "", isolation: "Shared" },
                  ])
                }
              >
                Add a sub-agent
              </Button>
            )}
          </Space>
        </Form.Item>

        {/*
          Labels, and the harness preview beside them.

          This is the part of the form that decides whether the template can be used
          at all, and nothing about a template says so — which is exactly why it is
          spelled out here rather than left as a metadata editor.
        */}
        <Form.Item
          label="Labels, and which harnesses will run this"
          extra="A harness admits templates through a label selector. A template no harness admits reaches no prepared revision, so no agent can ever be created from it."
        >
          <Space orientation="vertical" size={8} css={{ display: "flex" }}>
            {!readOnly && (harnesses.data ?? []).length > 0 ? (
              <Space size={8} wrap>
                <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                  Make it run on:
                </Text>
                {(harnesses.data ?? []).map((harness) => {
                  const admitted = wouldAdmit.some((match) => match.name === harness.name);
                  return (
                    <Button
                      key={harness.name}
                      size="small"
                      type={admitted ? "primary" : "default"}
                      data-testid={`template-form-admit-${harness.name}`}
                      disabled={admitted || Object.keys(harnessSelector(harness)).length === 0}
                      onClick={() => makeAdmittedBy(harness)}
                    >
                      {harness.name}
                    </Button>
                  );
                })}
              </Space>
            ) : null}

            {/* The preview is about *unsaved* labels. With nothing being edited there
                is nothing for it to be ahead of, and the page above states the
                controller's own answer instead. */}
            {readOnly ? null : (
              <div data-testid="template-form-admission">
                {wouldAdmit.length > 0 ? (
                  <Alert
                    type="success"
                    showIcon
                    title={`These labels are admitted by ${wouldAdmit
                      .map((harness) => harness.name)
                      .join(", ")}`}
                    description="An agent can be created from this template once the controller has prepared a revision for it."
                  />
                ) : (
                  <Alert
                    type="warning"
                    showIcon
                    title="No harness will run this template"
                    description="Nothing is wrong with the template itself — it simply carries no label any harness selects on, so no agent can be created from it. Use a button above, or add the labels by hand."
                  />
                )}
              </div>
            )}

            {readOnly && draft.labels.length === 0
              ? none("No labels — which is why no harness admits it.")
              : null}

            {draft.labels.map((label, index) => (
              <Space key={index} size={8} data-testid={`template-form-label-${index}`}>
                <Input
                  css={{ width: 260 }}
                  value={label.key}
                  placeholder={placeholder("kagent.dev/runtime")}
                  onChange={(event) => {
                    const next = [...draft.labels];
                    next[index] = { ...next[index], key: event.target.value };
                    set("labels", next);
                  }}
                  {...readOnlyInput}
                />
                <Input
                  css={{ width: 200 }}
                  value={label.value}
                  placeholder={placeholder("value")}
                  onChange={(event) => {
                    const next = [...draft.labels];
                    next[index] = { ...next[index], value: event.target.value };
                    set("labels", next);
                  }}
                  {...readOnlyInput}
                />
                {readOnly ? null : (
                  <Button
                    type="text"
                    aria-label={`Remove label ${index + 1}`}
                    icon={<Trash size={14} />}
                    onClick={() =>
                      set(
                        "labels",
                        draft.labels.filter((_, at) => at !== index),
                      )
                    }
                  />
                )}
              </Space>
            ))}
            {readOnly ? null : (
              <Button
                size="small"
                icon={<Plus size={13} />}
                data-testid="template-form-add-label"
                onClick={() => set("labels", [...draft.labels, { key: "", value: "" }])}
              >
                Add a label
              </Button>
            )}
          </Space>
        </Form.Item>

        {/* Said rather than left to be assumed. A reader who knows their template has
            skills and cannot see them here would reasonably conclude a save will
            drop them. */}
        {hasUnshownFields ? (
          <Alert
            type="info"
            showIcon
            data-testid="template-form-unshown"
            title="This template has skills, plugins or prompt data sources"
            description="This form does not author those yet, and saving does not remove them — they are carried through untouched. Edit them with kubectl."
            css={{ marginBottom: theme.space(4) }}
          />
        ) : null}

        {/* Nothing is being saved, so there is nothing to be not-ready to save. */}
        {!readOnly && problems.length > 0 ? (
          <Alert
            type="warning"
            showIcon
            data-testid="template-form-problems"
            title="Not ready to save"
            description={
              <ul css={{ margin: 0, paddingInlineStart: theme.space(4) }}>
                {problems.map((problem) => (
                  <li key={problem}>{problem}</li>
                ))}
              </ul>
            }
          />
        ) : null}
      </Form>

      <Paragraph css={{ margin: 0, color: theme.color.textMuted, fontSize: 12 }}>
        <Tag>AgentTemplate</Tag> is a <code>kagent.dev/v1alpha3</code> custom resource.
        Everything on this form writes one field of its <code>spec</code>, except the
        labels, which are <code>metadata</code> and decide which harness will run it.
      </Paragraph>
    </Space>
  );
}
