/**
 * What the agent-template form holds, and how it becomes a resource.
 *
 * ## The property that matters most here
 *
 * **An edit must not delete what the form does not show.** `AgentTemplateSpec` has
 * eight fields and this form authors five of them; `skills`, `plugins` and
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
}

/** One sub-agent binding, flattened for a form to hold. */
export interface AgentToolDraft {
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
  mcpTools: McpToolDraft[];
  agentTools: AgentToolDraft[];
  /**
   * The labels admission is decided by.
   *
   * Not decoration: a `Harness` admits templates through a label selector, and the
   * CRD says a harness with no selector admits none. A template whose labels match
   * nothing reaches no prepared revision and can never become an agent — so this is
   * the field that decides whether the template is usable at all.
   */
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
    mcpTools: [],
    agentTools: [],
    labels: [],
  };
}

/** The draft a form opens with when editing an existing template. */
export function draftFromTemplate(template: AgentTemplate): AgentTemplateDraft {
  const spec = template.resource.spec;
  const tools = spec.tools ?? [];

  return {
    name: template.name,
    namespace: template.namespace,
    modelConfig: spec.modelConfig?.name ?? "",
    description: spec.description ?? "",
    // Which one is in use is read from the resource rather than defaulted, so
    // reopening a template that reads its prompt from a ConfigMap does not silently
    // offer to replace it with an empty inline one.
    promptSource: spec.systemPromptFrom ? "configMap" : "inline",
    systemPrompt: spec.systemPrompt ?? "",
    systemPromptConfigMap: spec.systemPromptFrom?.name ?? "",
    systemPromptKey: spec.systemPromptFrom?.key ?? "",
    mcpTools: tools
      .filter((binding) => binding.mcp)
      .map((binding) => ({
        serverRef: binding.mcp?.server.name ?? "",
        tools: [...(binding.mcp?.tools ?? [])],
      })),
    agentTools: tools
      .filter((binding) => binding.agent)
      .map((binding) => ({
        name: binding.agent?.name ?? "",
        description: binding.agent?.description ?? "",
        templateName: binding.agent?.templateRef.name ?? "",
        isolation: binding.agent?.isolation ?? "Shared",
      })),
    labels: Object.entries(template.resource.metadata.labels ?? {}).map(
      ([key, value]) => ({ key, value }),
    ),
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
        },
      })),
    ...draft.agentTools
      .filter(
        (tool) => tool.name.trim() !== "" && tool.templateName.trim() !== "",
      )
      .map((tool) => ({
        agent: {
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
    modelConfig: { name: draft.modelConfig.trim() },
  };

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
  if (draft.modelConfig.trim() === "") {
    // The one genuinely required spec field.
    problems.push("A model configuration is required — every template must name one.");
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
  for (const tool of draft.agentTools) {
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
