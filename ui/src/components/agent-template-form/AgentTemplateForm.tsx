import { useMemo, type ReactNode } from "react";
import { Alert, Checkbox, Form, Input, Select, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useAgentTemplates, useMcpServers, useModels, useTools } from "@/api";
import { DataPill, DataPillList } from "@/components/form/DataPill";
import { FormField } from "@/components/form/FormField";
import { FormRow, FormRowList } from "@/components/form/FormRowList";
import { noValidation, type FieldValidation } from "@/components/form/useSubmitValidation";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import type { AgentTemplateDraft, OutputSource, TemplateField } from "./agentTemplateDraft";
import { SkillsField } from "./SkillsField";

import { referenceLabel, type ObjectReference } from "@/api/domain/agentTemplates";

const { Text } = Typography;

/** Picker values are strings; keep that representation at the form boundary. */
function referenceFromPicker(value: string): ObjectReference {
  const slash = value.indexOf("/");
  return slash < 0
    ? { name: value }
    : { name: value.slice(slash + 1), namespace: value.slice(0, slash) };
}

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
 * `plugins` and `promptTemplate` are not on this form. What matters is that an edit
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
  namespaceField,
  modelRequired = false,
  fieldFor = noValidation,
}: {
  draft: AgentTemplateDraft;
  onChange: (next: AgentTemplateDraft) => void;
  /** Create shows the name field; an edit cannot rename a Kubernetes object. */
  isCreate: boolean;
  namespace: string;
  /** Whether the template being edited carries spec fields this form does not show. */
  hasUnshownFields?: boolean;
  /** Render the same fields as text, for the details page before Edit is pressed. */
  readOnly?: boolean;
  /** Inline in an Agent: only spec fields, since an inline template has no name or labels. */
  embedded?: boolean;
  /** Rendered right after Name, so create forms read name then namespace. */
  namespaceField?: ReactNode;
  /** Marks the model required, as an Agent on a harness other than bring-your-own needs one. */
  modelRequired?: boolean;
  /** A field's message and touch callback, from `useSubmitValidation`. */
  fieldFor?: (field: TemplateField) => FieldValidation;
}) {
  const theme = useTheme();
  const models = useModels();
  const servers = useMcpServers();
  const tools = useTools();
  const templates = useAgentTemplates(namespace || undefined);

  const set = <K extends keyof AgentTemplateDraft>(
    field: K,
    value: AgentTemplateDraft[K],
  ) => onChange({ ...draft, [field]: value });

  // Read-only placeholders say "Not set": a grey example in a borderless input reads as a value.
  const placeholder = (example: string) => (readOnly ? "Not set" : example);

  const readOnlyInput = readOnly
    ? ({ readOnly: true, variant: "borderless" } as const)
    : {};
  const readOnlySelect = readOnly
    ? ({ open: false, variant: "borderless", suffixIcon: null } as const)
    : {};

  const modelOptions = (models.data ?? []).map((model) => {
    const name = model.ref;
    return {
      value: name,
      // `title` carries the label verbatim, which is what a spec locates an option by.
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
  });

  // A load error shows only when it leaves nothing to choose from. An empty list
  // may mean none exist or access hides them, so the hint names both.
  const modelHelp =
    !readOnly && models.error && (!models.data || modelOptions.length === 0) ? (
    "Model configurations could not be loaded. Bring-your-own (BYO) harnesses do not need one. If you need one, refresh the page."
  ) : !readOnly && !models.isLoading && modelOptions.length === 0 ? (
    <span data-testid="template-form-model-availability">
      No accessible model configurations found.{" "}
      Bring-your-own (BYO) harnesses do not need one.{" "}
      {isCreate
        ? "If you need one, create it or choose another namespace."
        : "If you need one, create it."} If
      one should be listed, check your access.
    </span>
  ) : readOnly ? undefined : (
    "Pick the ModelConfig to use. Shared ones must allow this namespace. Bring-your-own (BYO) harnesses skip this."
  );

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

  // A template cannot call itself, so an edit leaves its own name out of the list.
  const self = isCreate ? undefined : draft.name;
  const templateNames = (templates.data ?? [])
    .map((template) => template.name)
    .filter((name) => name !== self);

  /** "None", said in the place a list would have been. */
  const none = (what: string) => (
    <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{what}</Text>
  );

  const rowFields = { display: "flex", flexWrap: "wrap", gap: theme.space(2) } as const;
  const rowField = { marginBottom: 0 } as const;

  const updateMcp = (index: number, patch: Partial<AgentTemplateDraft["mcpTools"][number]>) =>
    set("mcpTools", draft.mcpTools.map((tool, at) => (at === index ? { ...tool, ...patch } : tool)));
  const updateSubAgent = (index: number, patch: Partial<AgentTemplateDraft["subAgentTools"][number]>) =>
    set("subAgentTools", draft.subAgentTools.map((tool, at) => (at === index ? { ...tool, ...patch } : tool)));
  const updateLabel = (index: number, patch: Partial<AgentTemplateDraft["labels"][number]>) =>
    set("labels", draft.labels.map((label, at) => (at === index ? { ...label, ...patch } : label)));

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

      <Form layout="vertical" component="div">
        {isCreate ? (
          <Field readOnly={readOnly}
            label="Name"
            required
            {...fieldFor("name")}
            extra="A Kubernetes object name, so it cannot be changed afterwards."
          >
            <Input
              data-testid="template-form-name"
              value={draft.name}
              onChange={(event) => set("name", event.target.value)}
              placeholder={placeholder("incident-responder")}
              {...readOnlyInput}
            />
          </Field>
        ) : null}

        {namespaceField}

        <Field readOnly={readOnly} label="Model configuration" required={modelRequired} extra={modelHelp} {...fieldFor("model")}>
          <Select
            data-testid="template-form-model"
            css={{ minWidth: 320 }}
            value={referenceLabel(draft.modelConfig, namespace) || undefined}
            loading={models.isLoading}
            placeholder={placeholder("Choose a model configuration")}
            popupMatchSelectWidth={false}
            onChange={(value: string) => set("modelConfig", referenceFromPicker(value))}
            options={modelOptions}
            {...readOnlySelect}
          />
        </Field>

        <Field readOnly={readOnly} label="Description" required={false}>
          <Input
            data-testid="template-form-description"
            value={draft.description}
            onChange={(event) => set("description", event.target.value)}
            placeholder={placeholder("What this agent is for.")}
            {...readOnlyInput}
          />
        </Field>

        <Field readOnly={readOnly}
          label="System prompt"
          required={false}
          extra={readOnly ? undefined : "Inline, or read from a ConfigMap. The CRD rejects a template that has both."}
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
                aria-label="System prompt"
                value={draft.systemPrompt}
                onChange={(event) => set("systemPrompt", event.target.value)}
                // Three rows is room to write in; reading, one row avoids a blank block.
                autoSize={{ minRows: readOnly ? 1 : 3, maxRows: 12 }}
                placeholder={placeholder(
                  "You are a Kubernetes operations assistant. Never guess.",
                )}
                {...readOnlyInput}
              />
            ) : (
              <div css={rowFields}>
                <Field readOnly={readOnly} label="ConfigMap name" required {...fieldFor("promptConfigMap")} css={rowField}>
                  <Input
                    data-testid="template-form-prompt-configmap"
                    value={draft.systemPromptConfigMap}
                    onChange={(event) => set("systemPromptConfigMap", event.target.value)}
                    placeholder={placeholder("agent-prompts")}
                    {...readOnlyInput}
                  />
                </Field>
                <Field readOnly={readOnly} label="Key" required {...fieldFor("promptKey")} css={rowField}>
                  <Input
                    data-testid="template-form-prompt-key"
                    value={draft.systemPromptKey}
                    onChange={(event) => set("systemPromptKey", event.target.value)}
                    placeholder={placeholder("system.md")}
                    {...readOnlyInput}
                  />
                </Field>
              </div>
            )}
          </Space>
        </Field>

        <Field readOnly={readOnly}
          label="Output format"
          required
          extra={readOnly ? undefined : "Structured output constrains only the successful final answer when this template runs as a root agent. It currently requires a kagent harness."}
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
              <Field readOnly={readOnly} label="Output schema" required {...fieldFor("outputSchema")} css={rowField}>
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
              </Field>
            ) : draft.outputSource === "configMap" ? (
              <div css={rowFields}>
                <Field readOnly={readOnly} label="ConfigMap name" required {...fieldFor("outputConfigMap")} css={rowField}>
                  <Input
                    data-testid="template-form-output-configmap"
                    value={draft.outputSchemaConfigMap}
                    onChange={(event) => set("outputSchemaConfigMap", event.target.value)}
                    placeholder={placeholder("agent-schemas")}
                    {...readOnlyInput}
                  />
                </Field>
                <Field readOnly={readOnly} label="Key" required {...fieldFor("outputKey")} css={rowField}>
                  <Input
                    data-testid="template-form-output-key"
                    value={draft.outputSchemaKey}
                    onChange={(event) => set("outputSchemaKey", event.target.value)}
                    placeholder={placeholder("schema.json")}
                    {...readOnlyInput}
                  />
                </Field>
              </div>
            ) : readOnly ? (
              none("Successful final answers are returned as text.")
            ) : null}

            {draft.outputSource === "text" ? null : readOnly ? (
              draft.outputStrict ? <DataPill>Strict schema adherence</DataPill> : null
            ) : (
              <Space orientation="vertical" size={4}>
                <Checkbox
                  checked={draft.outputStrict}
                  data-testid="template-form-output-strict"
                  onChange={(event) => set("outputStrict", event.target.checked)}
                >
                  Strict schema adherence
                </Checkbox>
                <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                  Asks OpenAI, Azure OpenAI and Foundry (OpenAI format) to follow the schema while
                  generating. Support depends on the vendor, so the model may not respect it. Other
                  providers never receive it, and OpenAI-compatible endpoints may ignore it. Every
                  answer is still checked against the schema. Strict requires a <code>type</code> on
                  every node, every property in <code>required</code> and{" "}
                  <code>additionalProperties: false</code> on every object.
                </Text>
              </Space>
            )}
          </Space>
        </Field>

        <Field readOnly={readOnly}
          label="MCP server tools"
          required={false}
          help={readOnly ? undefined : "Each binding names one server and optionally limits which tools to expose. An empty selection exposes every tool from that server. Require approval pauses before each of those tools runs."}
        >
          <FormRowList
            data-testid="template-form-mcp"
            empty={readOnly ? "No MCP tools." : undefined}
            addLabel="Add an MCP server"
            onAdd={readOnly ? undefined : () =>
              set("mcpTools", [...draft.mcpTools, { serverRef: { name: "" }, tools: [] }])
            }
          >
            {draft.mcpTools.map((tool, index) => (
              <FormRow
                key={index}
                data-testid={`template-form-mcp-${index}`}
                onRemove={readOnly ? undefined : () =>
                  set("mcpTools", draft.mcpTools.filter((_, at) => at !== index))
                }
                removeLabel={`Remove MCP server ${tool.serverRef.name || index + 1}`}
              >
                <div css={{ ...rowFields, alignItems: "flex-start" }}>
                  <Field readOnly={readOnly} label="MCP server" required {...fieldFor(`mcp:${index}:server`)} css={rowField}>
                    <Select
                      css={{ minWidth: 220 }}
                      data-testid={`template-form-mcp-server-${index}`}
                      value={referenceLabel(tool.serverRef, namespace) || undefined}
                      loading={servers.isLoading}
                      placeholder={placeholder("Choose an MCP server")}
                      popupMatchSelectWidth={false}
                      // The tools belong to the server, so changing it clears them.
                      onChange={(value: string) =>
                        updateMcp(index, { serverRef: referenceFromPicker(value), tools: [] })
                      }
                      options={(servers.data ?? []).map((server) => {
                        const bindable = server.groupKind.startsWith("RemoteMCPServer");
                        return {
                          value: server.ref,
                          title: bindable ? server.ref : `${server.ref} - unsupported server kind`,
                          label: bindable ? server.ref : `${server.ref} (unsupported)`,
                          disabled: !bindable,
                        };
                      })}
                      {...readOnlySelect}
                    />
                  </Field>
                  <Field readOnly={readOnly} label="Tools" required={false} css={rowField}>
                    <Select
                      mode="multiple"
                      css={{ minWidth: 320 }}
                      // Read-only pills carry no remove button, since nothing can be removed.
                      {...(readOnly
                        ? { tagRender: ({ label }: { label: ReactNode }) => <DataPill>{label}</DataPill> }
                        : {})}
                      value={tool.tools}
                      loading={tools.isLoading}
                      placeholder={placeholder("All tools")}
                      popupMatchSelectWidth={false}
                      onChange={(value: string[]) => updateMcp(index, { tools: value })}
                      options={(toolsByServer.get(referenceLabel(tool.serverRef, namespace)) ?? []).map((name) => ({
                        value: name,
                        title: name,
                        label: name,
                      }))}
                      {...readOnlySelect}
                    />
                  </Field>
                  {readOnly ? (
                    tool.requireApproval ? <DataPill>Requires approval</DataPill> : null
                  ) : (
                    <Checkbox
                      css={{ alignSelf: "center" }}
                      checked={Boolean(tool.requireApproval)}
                      data-testid={`template-form-mcp-approval-${index}`}
                      onChange={(event) => updateMcp(index, { requireApproval: event.target.checked })}
                    >
                      Require approval
                    </Checkbox>
                  )}
                </div>
              </FormRow>
            ))}
          </FormRowList>
        </Field>

        <Field readOnly={readOnly}
          label="Agent templates as tools"
          required={false}
          help={readOnly ? undefined : "Each row turns an agent template in this namespace into a tool. The description says when to call it."}
        >
          <FormRowList
            data-testid="template-form-sub-agents"
            empty={readOnly ? "No agent templates used as tools." : undefined}
            addLabel="Add an agent template"
            onAdd={readOnly ? undefined : () =>
              set("subAgentTools", [...draft.subAgentTools, { name: "", description: "", refName: "" }])
            }
          >
            {draft.subAgentTools.map((tool, index) => (
              <FormRow
                key={index}
                data-testid={`template-form-sub-agent-${index}`}
                onRemove={readOnly ? undefined : () =>
                  set("subAgentTools", draft.subAgentTools.filter((_, at) => at !== index))
                }
                removeLabel={`Remove agent template tool ${tool.name.trim() || index + 1}`}
              >
                <div css={{ ...rowFields, alignItems: "flex-start" }}>
                  <Field readOnly={readOnly} label="Agent template" required {...fieldFor(`subAgent:${index}:template`)} css={rowField}>
                    {readOnly ? (
                      <Input value={tool.refName} data-testid={`template-form-sub-agent-template-${index}`} {...readOnlyInput} />
                    ) : templates.error ? (
                      // The list could not be read, so the name is typed instead.
                      <Input
                        css={{ width: 200 }}
                        data-testid={`template-form-sub-agent-template-${index}`}
                        value={tool.refName}
                        placeholder="Type a template name"
                        onChange={(event) => updateSubAgent(index, { refName: event.target.value })}
                      />
                    ) : (
                      <div css={{ minWidth: 200 }}>
                        <ResourceSelect
                          kind="agent template"
                          names={templateNames}
                          loading={templates.isLoading}
                          value={tool.refName || undefined}
                          onChange={(refName) => updateSubAgent(index, { refName })}
                          empty="This namespace has no other agent templates."
                          data-testid={`template-form-sub-agent-template-${index}`}
                        />
                      </div>
                    )}
                  </Field>
                  <Field readOnly={readOnly} label="Tool name" required {...fieldFor(`subAgent:${index}:name`)} css={rowField}>
                    <Input
                      css={{ width: 160 }}
                      data-testid={`template-form-sub-agent-name-${index}`}
                      value={tool.name}
                      placeholder={placeholder("review")}
                      onChange={(event) => updateSubAgent(index, { name: event.target.value })}
                      {...readOnlyInput}
                    />
                  </Field>
                  <Field readOnly={readOnly} label="When to use it" required {...fieldFor(`subAgent:${index}:description`)} css={rowField}>
                    <Input
                      css={{ width: 260 }}
                      data-testid={`template-form-sub-agent-description-${index}`}
                      value={tool.description}
                      placeholder={placeholder("Review a change before it merges.")}
                      onChange={(event) => updateSubAgent(index, { description: event.target.value })}
                      {...readOnlyInput}
                    />
                  </Field>
                </div>
              </FormRow>
            ))}
          </FormRowList>
        </Field>

        <SkillsField skills={draft.skills} onChange={(next) => set("skills", next)} readOnly={readOnly} fieldFor={fieldFor} />

        {embedded ? null : (
          <Field readOnly={readOnly}
            label="Labels"
            required={false}
            help={readOnly ? undefined : "Optional metadata. An Agent explicitly pairs this template with a Harness."}
          >
            {readOnly ? (
              <DataPillList
                data-testid="template-form-labels"
                items={draft.labels.map(({ key, value }) => `${key}=${value}`)}
                empty={none("No labels.")}
              />
            ) : (
              <FormRowList
                data-testid="template-form-labels"
                addLabel="Add a label"
                onAdd={() => set("labels", [...draft.labels, { key: "", value: "" }])}
              >
                {draft.labels.map((label, index) => (
                  <FormRow
                    key={index}
                    data-testid={`template-form-label-${index}`}
                    onRemove={() => set("labels", draft.labels.filter((_, at) => at !== index))}
                    removeLabel={`Remove label ${label.key.trim() || index + 1}`}
                  >
                    <div css={{ ...rowFields, alignItems: "flex-start" }}>
                      <FormField label="Key" required {...fieldFor(`label:${index}:key`)} css={rowField}>
                        <Input
                          css={{ width: 260, fontFamily: theme.font.mono }}
                          value={label.key}
                          placeholder="kagent.dev/runtime"
                          onChange={(event) => updateLabel(index, { key: event.target.value })}
                        />
                      </FormField>
                      <FormField label="Value" required={false} css={rowField}>
                        <Input
                          css={{ width: 200, fontFamily: theme.font.mono }}
                          value={label.value}
                          placeholder="value"
                          onChange={(event) => updateLabel(index, { value: event.target.value })}
                        />
                      </FormField>
                    </div>
                  </FormRow>
                ))}
              </FormRowList>
            )}
          </Field>
        )}

        {/* Said rather than left to be assumed: a save keeps fields this form does not show. */}
        {hasUnshownFields ? (
          <Alert
            type="info"
            showIcon
            data-testid="template-form-unshown"
            title="This template has plugins or prompt data sources"
            description="This form does not author those yet, and saving does not remove them — they are carried through untouched. Edit them with kubectl."
            css={{ marginBottom: theme.space(4) }}
          />
        ) : null}
      </Form>
    </Space>
  );
}

/** A FormField while editing; reading is not a form, so labels drop the required and optional marks. */
function Field({ readOnly, required, error, onTouch, ...props }: Parameters<typeof FormField>[0] & { readOnly: boolean }) {
  return readOnly ? <Form.Item {...props} /> : <FormField required={required} error={error} onTouch={onTouch} {...props} />;
}
