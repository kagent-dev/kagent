import { useMemo } from "react";
import {
  Alert,
  Button,
  Checkbox,
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
  useMcpServers,
  useModels,
  useTools,
} from "@/api";
import {
  draftProblems,
  type AgentTemplateDraft,
  type OutputSource,
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
  embedded = false,
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
  /** Inline in an Agent: only spec fields, since an inline template has no name or labels. */
  embedded?: boolean;
}) {
  const theme = useTheme();
  const models = useModels();
  const servers = useMcpServers();
  const tools = useTools();

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

  /** "None", said in the place a list would have been. */
  const none = (what: string) => (
    <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{what}</Text>
  );

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
      {/* What this thing is, before any field. An AgentTemplate is half of an
          agent, and a reader who does not know that cannot tell why the form has
          no "run it" button. */}
      {embedded ? null : (
        <Alert
          type="info"
          showIcon
          data-testid="template-form-explainer"
          title="An agent template is what an agent does — not where it runs"
          description="It carries the model, the prompt and the tools. A harness carries the runtime: the adapter, the worker pool and the image. An Agent pairs one template with one harness."
        />
      )}

      <Form layout="vertical">
        {isCreate ? (
          <Form.Item
            label="Name"
            /* Unconditional: this field only exists while creating, and the only
               caller that creates does not render read-only. */
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
          extra="A ModelConfig in this template's own namespace. Every harness needs one except bring-your-own (BYO)."
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

        <Form.Item
          label="Output format"
          extra="Structured output constrains only the successful final answer when this template runs as a root agent. It currently requires a kagent harness."
        >
          <Space orientation="vertical" size={8} css={{ display: "flex" }}>
            <div data-testid="template-form-output-source">
              <Select
                css={{ minWidth: 240 }}
                value={draft.outputSource}
                onChange={(value: OutputSource) => set("outputSource", value)}
                options={[
                  { value: "text", title: "Text", label: "Text" },
                  {
                    value: "inline",
                    title: "Inline JSON Schema",
                    label: "Inline JSON Schema",
                  },
                  {
                    value: "configMap",
                    title: "JSON Schema from a ConfigMap",
                    label: "JSON Schema from a ConfigMap",
                  },
                ]}
                {...readOnlySelect}
              />
            </div>

            {draft.outputSource === "inline" ? (
              <Input.TextArea
                data-testid="template-form-output-schema"
                value={draft.outputSchema}
                onChange={(event) => set("outputSchema", event.target.value)}
                autoSize={{ minRows: readOnly ? 1 : 8, maxRows: 20 }}
                placeholder={placeholder(
                  '{\n  "type": "object",\n  "properties": {\n    "status": { "type": "string" }\n  },\n  "required": ["status"]\n}',
                )}
                css={{ fontFamily: theme.font.mono, fontSize: 12 }}
                {...readOnlyInput}
              />
            ) : draft.outputSource === "configMap" ? (
              <Space size={8}>
                <Input
                  data-testid="template-form-output-configmap"
                  value={draft.outputSchemaConfigMap}
                  onChange={(event) =>
                    set("outputSchemaConfigMap", event.target.value)
                  }
                  placeholder={placeholder("ConfigMap name")}
                  {...readOnlyInput}
                />
                <Input
                  data-testid="template-form-output-key"
                  value={draft.outputSchemaKey}
                  onChange={(event) => set("outputSchemaKey", event.target.value)}
                  placeholder={placeholder("Key")}
                  {...readOnlyInput}
                />
              </Space>
            ) : readOnly ? (
              none("Successful final answers are returned as text.")
            ) : null}
          </Space>
        </Form.Item>

        {/* Tools — MCP servers */}
        <Form.Item
          label="Tools from MCP servers"
          extra="Each binding names one server and optionally limits which tools to expose. An empty selection exposes every tool from that server. Require approval pauses before each of those tools runs."
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
                    next[index] = {
                      serverRef: value,
                      tools: [],
                      requireApproval: next[index].requireApproval,
                    };
                    set("mcpTools", next);
                  }}
                  options={(servers.data ?? []).map((server) => {
                    // Only a RemoteMCPServer can be bound — the compiler resolves no
                    // other kind. Command servers stay listed, so one that cannot be
                    // picked reads as a limitation rather than a missing row.
                    const bindable = server.groupKind.startsWith("RemoteMCPServer");
                    return {
                      value: server.ref,
                      title: bindable
                        ? server.ref
                        : `${server.ref} — a command server cannot be bound to an agent`,
                      label: bindable ? server.ref : `${server.ref} (command server)`,
                      disabled: !bindable,
                    };
                  })}
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
                {readOnly ? (
                  tool.requireApproval ? <Tag>Requires approval</Tag> : null
                ) : (
                  <Checkbox
                    checked={Boolean(tool.requireApproval)}
                    data-testid={`template-form-mcp-approval-${index}`}
                    onChange={(event) => {
                      const next = [...draft.mcpTools];
                      next[index] = {
                        ...next[index],
                        requireApproval: event.target.checked,
                      };
                      set("mcpTools", next);
                    }}
                  >
                    Require approval
                  </Checkbox>
                )}
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

        {embedded ? null : (
          <Form.Item label="Labels" extra="Optional metadata. An Agent explicitly pairs this template with a Harness.">
            <Space orientation="vertical" size={8} css={{ display: "flex" }}>
              {readOnly && draft.labels.length === 0
                ? none("No labels.")
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
        )}

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

      {embedded ? null : (
        <Paragraph css={{ margin: 0, color: theme.color.textMuted, fontSize: 12 }}>
          <Tag>AgentTemplate</Tag> is a <code>kagent.dev/v1alpha3</code> custom resource.
          Everything on this form writes one field of its <code>spec</code>, except the
          labels, which are <code>metadata</code>.
        </Paragraph>
      )}
    </Space>
  );
}
