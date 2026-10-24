/** Reusable agent behavior, independent of runtime configuration. */
import type { ResourceMetadata } from "./common";

/** A reference to a resource in the template's own namespace. Name-only, by construction. */
export interface AgentTemplateLocalRef {
  name: string;
}

/** A key in a same-namespace ConfigMap. */
export interface ConfigMapKeyRef {
  name: string;
  key: string;
}

/**
 * Tools selected from one MCP server.
 *
 * An omitted or empty `tools` list exposes every tool the server provides. A
 * non-empty list limits the binding to those names.
 */
export interface McpToolBinding {
  server: { kind: "RemoteMCPServer"; name: string };
  tools?: string[];
  requireApproval?: boolean;
}

/**
 * How the parent runs a sub-agent. `Shared` (the default) transfers the conversation
 * to it in the parent's Session. `Isolated` calls it as a tool in a new conversation
 * seeded with a copy of the parent's state: it never sees the parent's history,
 * parallel calls cannot interfere, and it cannot pause for human input, so its MCP
 * tools cannot require approval. Only the kagent and BYO harnesses support it.
 */
export type SubAgentIsolation = "Shared" | "Isolated";

/** Another AgentTemplate exposed as a tool in the parent's runtime. */
export interface SubAgentToolBinding {
  name: string;
  description: string;
  templateRef: AgentTemplateLocalRef;
  isolation?: SubAgentIsolation;
  // Deferred until Dedicated subagent execution is available.
  // agentRef?: AgentTemplateLocalRef;
}

/** Exactly one of `mcp` or `subAgent` — the CRD rejects both and neither. */
export interface ToolBinding {
  mcp?: McpToolBinding;
  subAgent?: SubAgentToolBinding;
}

/**
 * One immutable artifact, as `skills` and `plugins` reference content.
 *
 * Exactly one of `oci`, `git` or `bucket`. Left as-is by this build's form, which
 * does not author skills or plugins — see `AgentTemplateSpec.skills`.
 */
export interface ArtifactSource {
  oci?: string;
  git?: { url: string; commit: string };
  bucket?: {
    s3: {
      endpoint: string;
      bucket: string;
      key: string;
      versionId: string;
      region?: string;
    };
  };
  path?: string;
}

export interface AgentTemplateSkill {
  name: string;
  source: ArtifactSource;
}

export interface PluginBundle {
  source: ArtifactSource;
  skills?: string[];
}

/**
 * `spec.promptTemplate`: ConfigMaps a prompt may `include("source/key")`.
 *
 * Named `AgentTemplatePromptSpec` rather than `PromptTemplateSpec` because the
 * prompts domain already exports that name for an unrelated thing — a prompt
 * library. Two different `PromptTemplateSpec`s in one barrel export is a collision
 * the compiler catches and a reader would not.
 */
export interface AgentTemplatePromptSpec {
  dataSources?: { name: string; alias?: string }[];
}

/**
 * `AgentTemplateSpec`, field for field with `go/api/v1alpha3/agenttemplate_types.go`.
 *
 * Modelled in full even though the form authors only part of it, because the *edit*
 * path has to preserve what it does not show. Building an update from the fields a
 * form displays deletes every spec field it does not model — which is a mistake this
 * repository has already made once, and the reason `agentTemplateDraft` merges into
 * the existing spec rather than replacing it.
 */
export interface AgentTemplateSpec {
  /** A ModelConfig in the template's own namespace. Only BYO harnesses run without one. */
  modelConfig?: AgentTemplateLocalRef;
  description?: string;
  /** Mutually exclusive with `systemPromptFrom` — the CRD rejects both. */
  systemPrompt?: string;
  systemPromptFrom?: ConfigMapKeyRef;
  /**
   * A same-namespace ConfigMap key containing an `adk.declarative/v0.2` AgentSystem
   * document, as YAML or JSON. Only a root template on a BYO harness may set it, and
   * the CRD rejects it together with `systemPrompt` or `systemPromptFrom`. The form
   * does not author it; edits preserve it.
   */
  workflowFrom?: ConfigMapKeyRef;
  /**
   * The JSON Schema for a successful terminal response from this template when it
   * runs as the root agent. Mutually exclusive with `outputSchemaFrom`.
   */
  outputSchema?: Record<string, unknown>;
  /** A same-namespace ConfigMap key containing the output schema as JSON. */
  outputSchemaFrom?: ConfigMapKeyRef;
  /** Tells OpenAI, Azure OpenAI, and Foundry (OpenAI format) to limit generation to the schema. Requires a schema. */
  outputSchemaStrict?: boolean;
  promptTemplate?: AgentTemplatePromptSpec;
  tools?: ToolBinding[];
  skills?: AgentTemplateSkill[];
  plugins?: PluginBundle[];
}

export interface AgentTemplateResource {
  metadata: ResourceMetadata;
  spec: AgentTemplateSpec;
}

export interface AgentTemplate {
  /** `namespace/name`. */
  ref: string;
  namespace: string;
  name: string;

  /** `spec.modelConfig`, resolved into the template's namespace, as `namespace/name`. */
  modelConfigRef: string;

  description: string;


  /** The whole custom resource, which is what an edit form reads and writes. */
  resource: AgentTemplateResource;
}

export function templateLabels(template: AgentTemplate): Record<string, string> {
  return template.resource.metadata.labels ?? {};
}
