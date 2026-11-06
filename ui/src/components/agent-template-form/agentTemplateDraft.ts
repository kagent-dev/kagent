/**
 * What the agent-template form holds, and how it becomes a resource.
 *
 * ## The property that matters most here
 *
 * **An edit must not delete what the form does not show.** This form authors the
 * common interactive fields and `skills`; `plugins` and `promptTemplate` are not on
 * it yet.
 *
 * Building an update out of the fields a form displays silently drops the rest. That
 * has already happened in this repository once. `specFromDraft` therefore merges
 * into the spec it was given rather than constructing a fresh one, so a template carrying
 * plugins survives an edit that never mentioned them.
 *
 * `agentTemplateDraft.test.ts` pins that, because it is invisible on screen — the
 * form looks identical either way and the loss only shows up on the cluster.
 */

import type {
  AgentTemplate,
  AgentTemplateSkill,
  AgentTemplateSpec,
  ArtifactSource,
  ToolBinding,
  ObjectReference,
} from "@/api/domain/agentTemplates";
import type { BuiltinSkill, BuiltinSkillsCatalog } from "@/api/runtimeConfig";

/** One MCP tool selection, flattened for a form to hold. */
export interface McpToolDraft {
  /** The RemoteMCPServer reference; an omitted namespace means the draft namespace. */
  serverRef: ObjectReference;
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
  /** The template's bare name in the same namespace. */
  refName: string;
}

export type SkillSourceKind = "oci" | "git" | "s3";

/**
 * One skill, flattened for a form to hold. Every source kind keeps its own fields,
 * so switching kind and back does not lose what was typed.
 */
export interface SkillDraft {
  name: string;
  sourceKind: SkillSourceKind;
  oci: string;
  gitUrl: string;
  gitCommit: string;
  s3Endpoint: string;
  s3Bucket: string;
  s3Key: string;
  s3VersionId: string;
  s3Region: string;
  path: string;
}

type SkillTextField = Exclude<keyof SkillDraft, "sourceKind">;

/** The fields each source kind writes. Text in another kind's fields is ignored. */
const SKILL_SOURCE_FIELDS: Record<SkillSourceKind, SkillTextField[]> = {
  oci: ["oci"],
  git: ["gitUrl", "gitCommit"],
  s3: ["s3Endpoint", "s3Bucket", "s3Key", "s3VersionId", "s3Region"],
};

/** Where the system prompt comes from. The CRD rejects both at once. */
export type PromptSource = "inline" | "configMap";

/** Whether terminal output is prose or constrained by a JSON Schema. */
export type OutputSource = "text" | "inline" | "configMap";

export interface AgentTemplateDraft {
  name: string;
  namespace: string;
  /** The selected ModelConfig, absent when no model is selected. */
  modelConfig?: ObjectReference;
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
  outputStrict: boolean;
  mcpTools: McpToolDraft[];
  subAgentTools: SubAgentToolDraft[];
  skills: SkillDraft[];
  /**
   * The skill names `spec.plugins` selects. Not authored here, but the controller
   * rejects a skill named like one of them.
   */
  pluginSkills: string[];

  labels: { key: string; value: string }[];
}

export function emptyDraft(namespace: string): AgentTemplateDraft {
  return {
    name: "",
    namespace,
    modelConfig: undefined,
    description: "",
    promptSource: "inline",
    systemPrompt: "",
    systemPromptConfigMap: "",
    systemPromptKey: "",
    outputSource: "text",
    outputSchema: "",
    outputSchemaConfigMap: "",
    outputSchemaKey: "",
    outputStrict: false,
    mcpTools: [],
    subAgentTools: [],
    skills: [],
    pluginSkills: [],
    labels: [],
  };
}

export function emptySkillDraft(): SkillDraft {
  return {
    name: "",
    sourceKind: "oci",
    oci: "",
    gitUrl: "",
    gitCommit: "",
    s3Endpoint: "",
    s3Bucket: "",
    s3Key: "",
    s3VersionId: "",
    s3Region: "",
    path: "",
  };
}

/** One of kagent's built-in skills, as a row of the form. */
export function builtinSkillDraft(catalog: BuiltinSkillsCatalog, skill: BuiltinSkill): SkillDraft {
  return { ...emptySkillDraft(), name: skill.name, oci: catalog.image, path: skill.path };
}

/**
 * The built-in skill a row holds: the catalog image's repository and a catalog path.
 * `current` is false when the row pins another digest, as a row saved before a
 * chart upgrade does.
 */
export function matchBuiltinSkill(
  draft: SkillDraft,
  catalog: BuiltinSkillsCatalog | undefined,
): { skill: BuiltinSkill; current: boolean } | undefined {
  if (!catalog || draft.sourceKind !== "oci") return undefined;
  const image = draft.oci.trim();
  if (imageRepository(image) !== imageRepository(catalog.image)) return undefined;
  // The name must match too: a user may point their own skill at a catalog path under
  // another name, and that row must keep its editable name and source.
  const skill = catalog.skills.find(
    (entry) => entry.path === draft.path.trim() && entry.name === draft.name.trim(),
  );
  return skill && { skill, current: image === catalog.image };
}

function imageRepository(image: string): string {
  return image.split("@")[0];
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
    modelConfig: spec.modelConfig ? { ...spec.modelConfig } : undefined,
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
    outputStrict: spec.outputSchemaStrict ?? false,
    mcpTools: tools
      .filter((binding) => binding.mcp)
      .map((binding) => ({
        serverRef: { name: binding.mcp?.server.name ?? "", namespace: binding.mcp?.server.namespace },
        tools: [...(binding.mcp?.tools ?? [])],
        requireApproval: binding.mcp?.requireApproval,
      })),
    subAgentTools: tools
      .filter((binding) => binding.subAgent)
      .map((binding) => ({
        name: binding.subAgent?.name ?? "",
        description: binding.subAgent?.description ?? "",
        refName: binding.subAgent?.templateRef.name ?? "",
      })),
    skills: (spec.skills ?? []).map(skillDraftFromSpec),
    pluginSkills: (spec.plugins ?? []).flatMap((plugin) => plugin.skills ?? []),
    labels: [],
  };
}

/**
 * The spec a draft describes, merged onto the one it came from.
 *
 * `existing` is the whole reason this takes an argument. Omit it and every field
 * this form does not model — `plugins`, `promptTemplate` — is dropped from the
 * resource, which the API accepts happily and the reader never sees.
 */
export function specFromDraft(
  draft: AgentTemplateDraft,
  existing?: AgentTemplateSpec,
): AgentTemplateSpec {
  const tools: ToolBinding[] = [
    ...draft.mcpTools
      // A named server with no explicit selection exposes all of its tools. Only an
      // unfinished row with no server is dropped.
      .filter((tool) => tool.serverRef.name.trim() !== "")
      .map((tool) => ({
        mcp: {
          // The only kind the CRD's enum allows.
          server: { kind: "RemoteMCPServer" as const, ...normalizedReference(tool.serverRef, draft.namespace) },
          ...(tool.tools.length > 0 ? { tools: [...tool.tools] } : {}),
          // omitempty on the CRD: false is the default, so only true is sent.
          ...(tool.requireApproval ? { requireApproval: true } : {}),
        },
      })),
    ...draft.subAgentTools
      .filter(
        (tool) => tool.name.trim() !== "" && tool.refName.trim() !== "",
      )
      .map((tool) => ({
        subAgent: {
          name: tool.name.trim(),
          description: tool.description.trim(),
          templateRef: { name: tool.refName.trim() },
        },
      })),
  ];

  const spec: AgentTemplateSpec = {
    // Everything the form does not model, carried over untouched.
    ...(existing ?? {}),
  };
  const model = draft.modelConfig;
  if (!model?.name.trim()) delete spec.modelConfig;
  else spec.modelConfig = normalizedReference(model, draft.namespace);

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
  if (draft.outputStrict && (spec.outputSchema || spec.outputSchemaFrom)) spec.outputSchemaStrict = true;
  else delete spec.outputSchemaStrict;

  // An empty list is removed rather than sent: `tools: []` and no `tools` mean the
  // same thing to the controller, and the shorter resource is the one a reader can
  // read.
  if (tools.length > 0) spec.tools = tools;
  else delete spec.tools;

  const skills = draft.skills.filter((skill) => !isBlankSkill(skill)).map(skillFromDraft);
  if (skills.length > 0) spec.skills = skills;
  else delete spec.skills;

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
  problems.push(...skillProblems(draft.skills, draft.pluginSkills));
  return problems;
}

/** Mirrors the CRD's `AgentTemplateSkill` and `ArtifactSource` validation. */
const MAX_SKILLS = 50;
const MAX_PATH_LENGTH = 1024;
const OCI_DIGEST_REF = /^[^\s@]+@sha256:[0-9a-f]{64}$/;
const HTTP_URL = /^https?:\/\/\S+$/;
const GIT_COMMIT = /^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$/;

function skillProblems(drafts: SkillDraft[], pluginSkills: string[]): string[] {
  const problems: string[] = [];
  if (drafts.filter((skill) => !isBlankSkill(skill)).length > MAX_SKILLS) {
    problems.push(`A template can have at most ${MAX_SKILLS} skills.`);
  }

  const seen = new Set<string>();
  const repeated = new Set<string>();
  const fromPlugins = new Set<string>();
  drafts.forEach((draft, index) => {
    if (isBlankSkill(draft)) return;
    const skill = skillFromDraft(draft);
    const label = skill.name ? `The skill "${skill.name}"` : `Skill ${index + 1}`;
    if (!skill.name) problems.push(`${label} needs a name.`);
    else if (skill.name === "." || skill.name === ".." || /[/\\]/.test(skill.name)) {
      problems.push(`${label} needs a name that is one path segment: not "." or "..", and without "/" or "\\".`);
    } else if (seen.has(skill.name)) repeated.add(skill.name);
    else if (pluginSkills.includes(skill.name)) fromPlugins.add(skill.name);
    seen.add(skill.name);

    const { source } = skill;
    if (draft.sourceKind === "oci" && !OCI_DIGEST_REF.test(source.oci ?? "")) {
      problems.push(`${label} needs an OCI image pinned by digest, like ghcr.io/org/skills@sha256:<64 hex characters>.`);
    }
    if (draft.sourceKind === "git") {
      if (!HTTP_URL.test(source.git?.url ?? "")) {
        problems.push(`${label} needs a Git URL that starts with http:// or https://.`);
      }
      if (!GIT_COMMIT.test(source.git?.commit ?? "")) {
        problems.push(`${label} needs a full Git commit ID: 40 or 64 hex characters.`);
      }
    }
    if (draft.sourceKind === "s3") {
      const s3 = source.bucket?.s3;
      if (!HTTP_URL.test(s3?.endpoint ?? "")) {
        problems.push(`${label} needs an S3 endpoint that starts with http:// or https://.`);
      }
      const missing = [
        s3?.bucket ? "" : "bucket",
        s3?.key ? "" : "key",
        s3?.versionId ? "" : "version ID",
      ].filter(Boolean);
      if (missing.length > 0) {
        const fields = missing.length === 1 ? missing[0] : `${missing.slice(0, -1).join(", ")} and ${missing.at(-1)}`;
        problems.push(`${label} needs an S3 ${fields}.`);
      }
    }
    const path = source.path ?? "";
    if (path.startsWith("/") || path.split("/").includes("..")) {
      problems.push(`${label} needs a path relative to the artifact, without ".." segments.`);
    }
    if (path.length > MAX_PATH_LENGTH) {
      problems.push(`${label} needs a path of at most ${MAX_PATH_LENGTH} characters.`);
    }
  });
  for (const name of repeated) {
    problems.push(`More than one skill is named "${name}". Skill names must be unique.`);
  }
  for (const name of fromPlugins) {
    problems.push(`A plugin on this template already selects a skill named "${name}". Skill names must be unique.`);
  }
  return problems;
}

function skillDraftFromSpec(skill: AgentTemplateSkill): SkillDraft {
  const { source } = skill;
  const s3 = source.bucket?.s3;
  return {
    name: skill.name,
    sourceKind: source.git ? "git" : s3 ? "s3" : "oci",
    oci: source.oci ?? "",
    gitUrl: source.git?.url ?? "",
    gitCommit: source.git?.commit ?? "",
    s3Endpoint: s3?.endpoint ?? "",
    s3Bucket: s3?.bucket ?? "",
    s3Key: s3?.key ?? "",
    s3VersionId: s3?.versionId ?? "",
    s3Region: s3?.region ?? "",
    path: source.path ?? "",
  };
}

function skillFromDraft(draft: SkillDraft): AgentTemplateSkill {
  let source: ArtifactSource;
  if (draft.sourceKind === "git") {
    source = { git: { url: draft.gitUrl.trim(), commit: draft.gitCommit.trim() } };
  } else if (draft.sourceKind === "s3") {
    const region = draft.s3Region.trim();
    source = {
      bucket: {
        s3: {
          endpoint: draft.s3Endpoint.trim(),
          bucket: draft.s3Bucket.trim(),
          key: draft.s3Key.trim(),
          versionId: draft.s3VersionId.trim(),
          ...(region ? { region } : {}),
        },
      },
    };
  } else {
    source = { oci: draft.oci.trim() };
  }
  const path = draft.path.trim();
  if (path) source.path = path;
  return { name: draft.name.trim(), source };
}

/** A row the reader added and never filled in, which a save drops. */
function isBlankSkill(draft: SkillDraft): boolean {
  const fields: SkillTextField[] = ["name", "path", ...SKILL_SOURCE_FIELDS[draft.sourceKind]];
  return fields.every((field) => draft[field].trim() === "");
}

/** Keep local manifests concise while preserving shared resource identity. */
function normalizedReference(ref: ObjectReference, ownerNamespace: string): ObjectReference {
  const name = ref.name.trim();
  const namespace = ref.namespace?.trim();
  return !namespace || namespace === ownerNamespace ? { name } : { name, namespace };
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
