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

/** Another AgentTemplate exposed to this one as a tool it can route work to. */
export interface AgentToolBinding {
  name: string;
  description: string;
  templateRef: AgentTemplateLocalRef;
  /**
   * Whether the referenced template shares this one's runtime boundary.
   *
   * `Shared` is the CRD's default. `Dedicated` gives the sub-agent its own
   * boundary, which costs a separate runtime and isolates it.
   */
  isolation?: "Shared" | "Dedicated";
}

/** Exactly one of `mcp` or `agent` — the CRD rejects both and neither. */
export interface ToolBinding {
  mcp?: McpToolBinding;
  agent?: AgentToolBinding;
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
   * The JSON Schema for a successful terminal response from this template when it
   * runs as the root agent. Mutually exclusive with `outputSchemaFrom`.
   */
  outputSchema?: Record<string, unknown>;
  /** A same-namespace ConfigMap key containing the output schema as JSON. */
  outputSchemaFrom?: ConfigMapKeyRef;
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
