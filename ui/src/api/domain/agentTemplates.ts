/**
 * An `AgentTemplate`: the behaviour half of what an agent is made of.
 *
 * The other half is a [`Harness`](./harnesses.ts). A template says what the agent
 * *is* — model configuration, system prompt, tools, skills, plugins — and carries
 * nothing about where it runs; the harness says how it runs and nothing about what
 * it does. `CreateAgentInstance` names one of each, and the running agent is the
 * pair.
 *
 * ## A template nothing admits cannot be used
 *
 * This is the single most important thing about authoring one, and nothing on the
 * template itself says it. A `Harness` admits templates through a **label
 * selector** (`spec.allowedAgentTemplates.selector`), and the CRD is explicit that
 * *"when omitted, the Harness admits none"*. So a template whose labels match no
 * harness reaches `status.harnesses = []`, no prepared revision is ever built for
 * it, and `CreateAgentInstance` refuses the pair with `FailedPrecondition`.
 *
 * Confirmed on a cluster rather than read off the CRD: a template applied with no
 * labels came back with `status: {observedGeneration: 1}` and nothing else; adding
 * the one label its harness selects on took it to *"ActorTemplate golden snapshot
 * is ready"* within ten seconds.
 *
 * That is why `labels` are part of the domain type and part of the form, and why
 * the form offers to set them *from a harness* rather than only as free text.
 */

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
 * `tools` is required and must hold at least one name — unlike the tool bindings on
 * the older `Agent` kind, where an empty list meant *every* tool the server
 * exposes. The CRD here has `MinItems=1`, so "all of them" has to be spelled out.
 */
export interface McpToolBinding {
  server: { kind: "RemoteMCPServer"; name: string };
  tools: string[];
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
  /** Required. A ModelConfig in the template's own namespace. */
  modelConfig: AgentTemplateLocalRef;
  description?: string;
  /** Mutually exclusive with `systemPromptFrom` — the CRD rejects both. */
  systemPrompt?: string;
  systemPromptFrom?: ConfigMapKeyRef;
  promptTemplate?: AgentTemplatePromptSpec;
  tools?: ToolBinding[];
  skills?: AgentTemplateSkill[];
  plugins?: PluginBundle[];
}

/** One condition the controller recorded for a template under one harness. */
export interface AgentTemplateCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
}

/** What the controller made of this template for one admitting harness. */
export interface AgentTemplateHarnessStatus {
  harness: string;
  desiredRevision?: string;
  latestSuccessfulRevision?: string;
  conditions?: AgentTemplateCondition[];
}

export interface AgentTemplateResource {
  metadata: ResourceMetadata;
  spec: AgentTemplateSpec;
  status?: {
    observedGeneration?: number;
    harnesses?: AgentTemplateHarnessStatus[];
  };
}

export interface AgentTemplate {
  /** `namespace/name`. */
  ref: string;
  namespace: string;
  name: string;

  /** `spec.modelConfig`, resolved into the template's namespace, as `namespace/name`. */
  modelConfigRef: string;

  description: string;

  /**
   * The harnesses that will accept this template, by name, within its namespace.
   *
   * Reported in the template's status and derivable only from the harness side — a
   * harness admits templates through a label selector, so nothing about a template
   * says which ones match it. This is therefore the set a caller may legally pair
   * it with, and it cannot be computed here.
   *
   * **Empty means the template cannot be used at all**, not merely that a list is
   * short: with no admitting harness there is no prepared revision, and every
   * `CreateAgentInstance` naming it is refused.
   */
  admittingHarnesses: string[];

  /** The whole custom resource, which is what an edit form reads and writes. */
  resource: AgentTemplateResource;
}

/**
 * Whether a harness may be paired with a template.
 *
 * Asked before the pair is offered rather than after it is refused:
 * `CreateAgentInstance` answers `FailedPrecondition` for a pair the controller
 * does not admit, and a picker that let a reader choose one and then reported
 * that would be offering a choice it knew was invalid.
 *
 * Matched on the harness's bare name, which is what `admitting_harnesses`
 * carries — a harness admits templates in its own namespace only, so a namespace
 * on either side would never differ and comparing full refs would never match.
 */
export function admitsHarness(template: AgentTemplate, harnessName: string): boolean {
  return template.admittingHarnesses.includes(harnessName);
}

/**
 * Whether any harness will run this template.
 *
 * The question every template list has to answer, because the alternative — a row
 * that looks like every other row and cannot be turned into an agent — is exactly
 * the kind of quiet half-truth this codebase keeps having to undo.
 */
export function isUsable(template: AgentTemplate): boolean {
  return template.admittingHarnesses.length > 0;
}

/** The labels on the template, which are what admission is decided by. */
export function templateLabels(template: AgentTemplate): Record<string, string> {
  return template.resource.metadata.labels ?? {};
}
