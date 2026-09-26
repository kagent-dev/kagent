/**
 * What the agent-template form holds, and how it becomes a resource.
 *
 * ## The property that matters most here
 *
 * **An edit must not delete what the form does not show.** This form authors the
 * common interactive fields; `skills`, `plugins` and
 * `promptTemplate` are rich enough — three artifact-source shapes, each with its own
 * strict CEL pattern — that authoring them is its own piece of work.
 *
 * Building an update out of the fields a form displays silently drops the rest. That
 * has already happened in this repository once. `specFromDraft` therefore merges
 * into the spec it was given rather than constructing a fresh one, so a template carrying
 * skills survives an edit that never mentioned them.
 *
 * `agentTemplateDraft.test.ts` pins that, because it is invisible on screen — the
 * form looks identical either way and the loss only shows up on the cluster.
 */

import type {
  AgentTemplate,
  AgentTemplateSpec,
  ToolBinding,
} from "@/api/domain/agentTemplates";

/** One MCP tool selection, flattened for a form to hold. */
export interface McpToolDraft {
  /** `namespace/name` of the RemoteMCPServer, as the tool list reports it. */
  serverRef: string;
  /** The tool names selected. Empty means every tool the server exposes. */
  tools: string[];
  /** Pause before each invocation of a tool this binding exposes. */
  requireApproval?: boolean;
}

/** One sub-agent binding, flattened for a form to hold. */
export interface SubAgentToolDraft {
  /** What the parent calls this tool. */
  name: string;
  /** When the parent should route work to it — the CRD requires this. */
  description: string;
  /** The AgentTemplate it points at, by bare name in the same namespace. */
  templateName: string;
  isolation: "Shared" | "Dedicated";
}

/** Where the system prompt comes from. The CRD rejects both at once. */
export type PromptSource = "inline" | "configMap";

/** Whether terminal output is prose or constrained by a JSON Schema. */
export type OutputSource = "text" | "inline" | "configMap";

export interface AgentTemplateDraft {
  name: string;
  namespace: string;
  /** The ModelConfig, by bare name — the CRD's reference is same-namespace. */
  modelConfig: string;
  description: string;
  promptSource: PromptSource;
  systemPrompt: string;
  systemPromptConfigMap: string;
  systemPromptKey: string;
  outputSource: OutputSource;
  /** Pretty-printed JSON while loaded; kept as text so incomplete edits remain editable. */
  outputSchema: string;
  outputSchemaConfigMap: string;
  outputSchemaKey: string;
  mcpTools: McpToolDraft[];
  subAgentTools: SubAgentToolDraft[];

  labels: { key: string; value: string }[];
}

export function emptyDraft(namespace: string): AgentTemplateDraft {
  return {
    name: "",
    namespace,
    modelConfig: "",
    description: "",
    promptSource: "inline",
    systemPrompt: "",
    systemPromptConfigMap: "",
    systemPromptKey: "",
    outputSource: "text",
    outputSchema: "",
    outputSchemaConfigMap: "",
    outputSchemaKey: "",
    mcpTools: [],
    subAgentTools: [],
    labels: [],
  };
}

/** The draft a form opens with when editing an existing template. */
export function draftFromTemplate(template: AgentTemplate): AgentTemplateDraft {
  return {
    ...draftFromSpec(template.resource.spec, template.namespace),
    name: template.name,
    labels: Object.entries(template.resource.metadata.labels ?? {}).map(
      ([key, value]) => ({ key, value }),
    ),
  };
}

/** The draft for a bare spec, such as an Agent's inline template, which has no name or labels. */
export function draftFromSpec(spec: AgentTemplateSpec, namespace: string): AgentTemplateDraft {
  const tools = spec.tools ?? [];

  return {
    name: "",
    namespace,
    modelConfig: spec.modelConfig?.name ?? "",
    description: spec.description ?? "",
    // Which one is in use is read from the resource rather than defaulted, so
    // reopening a template that reads its prompt from a ConfigMap does not silently
    // offer to replace it with an empty inline one.
    promptSource: spec.systemPromptFrom ? "configMap" : "inline",
    systemPrompt: spec.systemPrompt ?? "",
    systemPromptConfigMap: spec.systemPromptFrom?.name ?? "",
    systemPromptKey: spec.systemPromptFrom?.key ?? "",
    outputSource: spec.outputSchemaFrom
      ? "configMap"
      : spec.outputSchema
        ? "inline"
        : "text",
    outputSchema: spec.outputSchema
      ? JSON.stringify(spec.outputSchema, null, 2)
      : "",
    outputSchemaConfigMap: spec.outputSchemaFrom?.name ?? "",
    outputSchemaKey: spec.outputSchemaFrom?.key ?? "",
    mcpTools: tools
      .filter((binding) => binding.mcp)
      .map((binding) => ({
        serverRef: binding.mcp?.server.name ?? "",
        tools: [...(binding.mcp?.tools ?? [])],
        requireApproval: binding.mcp?.requireApproval,
      })),
    subAgentTools: tools
      .filter((binding) => binding.subAgent)
      .map((binding) => ({
        name: binding.subAgent?.name ?? "",
        description: binding.subAgent?.description ?? "",
        templateName: binding.subAgent?.templateRef.name ?? "",
        isolation: binding.subAgent?.isolation ?? "Shared",
      })),
    labels: [],
  };
}

/**
 * The spec a draft describes, merged onto the one it came from.
 *
 * `existing` is the whole reason this takes an argument. Omit it and every field
 * this form does not model — `skills`, `plugins`, `promptTemplate` — is dropped from
 * the resource, which the API accepts happily and the reader never sees.
 */
export function specFromDraft(
  draft: AgentTemplateDraft,
  existing?: AgentTemplateSpec,
): AgentTemplateSpec {
  const tools: ToolBinding[] = [
    ...draft.mcpTools
      // A named server with no explicit selection exposes all of its tools. Only an
      // unfinished row with no server is dropped.
      .filter((tool) => tool.serverRef.trim() !== "")
      .map((tool) => ({
        mcp: {
          // The only kind the CRD's enum allows.
          server: { kind: "RemoteMCPServer" as const, name: bareName(tool.serverRef) },
          ...(tool.tools.length > 0 ? { tools: [...tool.tools] } : {}),
          // omitempty on the CRD: false is the default, so only true is sent.
          ...(tool.requireApproval ? { requireApproval: true } : {}),
        },
      })),
    ...draft.subAgentTools
      .filter(
        (tool) => tool.name.trim() !== "" && tool.templateName.trim() !== "",
      )
      .map((tool) => ({
        subAgent: {
          name: tool.name.trim(),
          description: tool.description.trim(),
          templateRef: { name: tool.templateName.trim() },
          isolation: tool.isolation,
        },
      })),
  ];

  const spec: AgentTemplateSpec = {
    // Everything the form does not model, carried over untouched.
    ...(existing ?? {}),
  };
  const model = draft.modelConfig.trim();
  if (model === "") delete spec.modelConfig;
  else spec.modelConfig = { name: model };

  setOrDelete(spec, "description", draft.description.trim());

  /*
   * The two prompt sources are mutually exclusive, and the CRD enforces it.
   *
   * So the one not chosen is *removed* rather than left as it was: switching a
   * template from a ConfigMap prompt to an inline one while `systemPromptFrom`
   * survived in the spec would be rejected outright, and the message names a
   * validation rule rather than the switch the reader just used.
   */
  if (draft.promptSource === "configMap") {
    delete spec.systemPrompt;
    const name = draft.systemPromptConfigMap.trim();
    const key = draft.systemPromptKey.trim();
    if (name && key) spec.systemPromptFrom = { name, key };
    else delete spec.systemPromptFrom;
  } else {
    delete spec.systemPromptFrom;
    setOrDelete(spec, "systemPrompt", draft.systemPrompt.trim());
  }


  if (draft.outputSource === "configMap") {
    delete spec.outputSchema;
    const name = draft.outputSchemaConfigMap.trim();
    const key = draft.outputSchemaKey.trim();
    if (name && key) spec.outputSchemaFrom = { name, key };
    else delete spec.outputSchemaFrom;
  } else if (draft.outputSource === "inline") {
    delete spec.outputSchemaFrom;
    const schema = parseOutputSchema(draft.outputSchema);
    if (schema) spec.outputSchema = schema;
    else delete spec.outputSchema;
  } else {
    delete spec.outputSchema;
    delete spec.outputSchemaFrom;
  }

  // An empty list is removed rather than sent: `tools: []` and no `tools` mean the
  // same thing to the controller, and the shorter resource is the one a reader can
  // read.
  if (tools.length > 0) spec.tools = tools;
  else delete spec.tools;

  return spec;
}

/** The labels a draft describes, as the resource carries them. */
export function labelsFromDraft(draft: AgentTemplateDraft): Record<string, string> {
  const labels: Record<string, string> = {};
  for (const { key, value } of draft.labels) {
    const name = key.trim();
    if (name !== "") labels[name] = value.trim();
  }
  return labels;
}

/**
 * What is wrong with the draft, in the order a reader would fix it.
 *
 * Only the rules the controller will actually refuse. A form that invented its own
 * would block a template the cluster would have accepted.
 */
export function draftProblems(
  draft: AgentTemplateDraft,
  options: { isCreate: boolean },
): string[] {
  const problems: string[] = [];

  if (options.isCreate && draft.name.trim() === "") {
    problems.push("A name is required.");
  }
  if (draft.namespace.trim() === "") {
    problems.push("A namespace is required.");
  }
  if (draft.promptSource === "configMap") {
    const name = draft.systemPromptConfigMap.trim();
    const key = draft.systemPromptKey.trim();
    if ((name === "") !== (key === "")) {
      problems.push(
        "A prompt read from a ConfigMap needs both the ConfigMap's name and the key inside it.",
      );
    }
  }
  if (draft.outputSource === "configMap") {
    const name = draft.outputSchemaConfigMap.trim();
    const key = draft.outputSchemaKey.trim();
    if (name === "" || key === "") {
      problems.push(
        "An output schema read from a ConfigMap needs both the ConfigMap's name and the key inside it.",
      );
    }
  }
  if (draft.outputSource === "inline") {
    const text = draft.outputSchema.trim();
    if (text === "") {
      problems.push("An inline output schema is required.");
    } else {
      const schema = parseOutputSchema(text);
      if (!schema) {
        problems.push("The inline output schema must be a valid JSON object.");
      } else if (schema.type !== "object") {
        problems.push('The output schema must have "type": "object" at its root.');
      }
    }
  }
  for (const tool of draft.subAgentTools) {
    if (tool.name.trim() !== "" && tool.description.trim() === "") {
      problems.push(
        `The sub-agent tool "${tool.name.trim()}" needs a description — it is what tells the parent when to use it.`,
      );
    }
  }
  return problems;
}

/** `namespace/name` → `name`. The CRD's references are same-namespace and name-only. */
function bareName(ref: string): string {
  const slash = ref.lastIndexOf("/");
  return slash === -1 ? ref : ref.slice(slash + 1);
}

/** Parses only the object-valued JSON shape the public API accepts. */
function parseOutputSchema(text: string): Record<string, unknown> | undefined {
  try {
    const value: unknown = JSON.parse(text);
    if (value === null || typeof value !== "object" || Array.isArray(value)) {
      return undefined;
    }
    return value as Record<string, unknown>;
  } catch {
    return undefined;
  }
}

/**
 * Sets a spec field, or removes it when the value is empty.
 *
 * Removing rather than writing `""` because the two are not the same resource: an
 * empty string is a value the CRD stores and shows back, where an absent field is
 * the default. A form that wrote empties would fill a template with keys nobody set.
 */
function setOrDelete<K extends keyof AgentTemplateSpec>(
  spec: AgentTemplateSpec,
  field: K,
  value: string,
): void {
  if (value === "") delete spec[field];
  else spec[field] = value as AgentTemplateSpec[K];
}
