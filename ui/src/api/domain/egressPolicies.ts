/** `EgressPolicy`, field for field with `go/api/v1alpha3/egresspolicy_types.go`. */
import type { ActorJWT, ResourceMetadata, SecretKeySelector } from "./common";
import type { Agent, AgentCondition } from "./agents";
import type { SandboxTemplate } from "./sandboxTemplates";

/** Exactly one of `secretKeyRef` or `actorJwt`. */
export interface EgressAuthentication {
  header: string;
  prefix?: string;
  secretKeyRef?: SecretKeySelector;
  actorJwt?: ActorJWT;
}

export interface EgressDestination {
  origin: string;
  authentication?: EgressAuthentication;
}

export interface EgressPolicySpec {
  destinations: EgressDestination[];
}

export interface EgressPolicyResource {
  apiVersion?: string;
  kind?: string;
  metadata: ResourceMetadata;
  spec: EgressPolicySpec;
}

export interface EgressPolicy {
  /** `namespace/name`. */
  ref: string;
  namespace: string;
  name: string;
  resource: EgressPolicyResource;
}

// region CRD rules

/** The CRD's limits, so the form and the server agree. */
export const EGRESS_LIMITS = {
  destinations: 64,
  originLength: 270,
  headerLength: 256,
  prefixLength: 256,
  audiences: 16,
  audienceLength: 512,
  minExpirationSeconds: 300,
  maxExpirationSeconds: 3600,
} as const;

const LABEL = "[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?";
const PORT = "(:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?";
/** The `origin` pattern from the CRD, verbatim. */
export const EGRESS_ORIGIN = new RegExp(
  `^https?://(\\*\\.(${LABEL}\\.)+|(${LABEL}\\.)*)[a-z]([-a-z0-9]{0,61}[a-z0-9])?${PORT}$`,
);
const HEADER_TOKEN = /^[!#$%&'*+.^_|~0-9A-Za-z-]+$/;
// Tab is allowed; every other control character is not.
// eslint-disable-next-line no-control-regex
const PREFIX_TEXT = /^[^\x00-\x08\x0a-\x1f\x7f]*$/;

/** Headers the CRD refuses to carry a credential in, compared case-insensitively. */
export const BLOCKED_EGRESS_HEADERS = [
  "host",
  "connection",
  "content-length",
  "transfer-encoding",
  "te",
  "trailer",
  "upgrade",
  "keep-alive",
  "proxy-authorization",
  "proxy-authenticate",
  "cookie",
] as const;

/** The runtime control-plane audience, reserved by the CRD. */
export const RESERVED_AUDIENCE = "kagent-runtime";

export const isWildcardOrigin = (origin: string) => origin.includes("*");

/** Why one destination would be rejected, in the order a reader fixes them. */
export function destinationProblems(destination: EgressDestination): string[] {
  const problems: string[] = [];
  const { origin, authentication } = destination;
  if (!origin) problems.push("Enter an origin.");
  else if (origin.length > EGRESS_LIMITS.originLength || !EGRESS_ORIGIN.test(origin)) {
    problems.push("Use a scheme and host, like https://api.example.com. No path, IP or user.");
  }
  if (!authentication) return problems;

  if (isWildcardOrigin(origin)) problems.push("A wildcard origin can't use authentication.");
  const { header, prefix, secretKeyRef, actorJwt } = authentication;
  if (!header) problems.push("Enter a header name.");
  else if (header.length > EGRESS_LIMITS.headerLength || !HEADER_TOKEN.test(header)) {
    problems.push("The header name has characters that aren't allowed.");
  } else if ((BLOCKED_EGRESS_HEADERS as readonly string[]).includes(header.toLowerCase())) {
    problems.push(`The ${header} header can't carry a credential.`);
  }
  if (prefix !== undefined && (prefix.length > EGRESS_LIMITS.prefixLength || !PREFIX_TEXT.test(prefix))) {
    problems.push("The prefix is too long or has control characters.");
  }
  if (Number(Boolean(secretKeyRef)) + Number(Boolean(actorJwt)) !== 1) {
    problems.push("Choose a Secret key or an Actor JWT.");
  }
  if (secretKeyRef && (!secretKeyRef.name || !secretKeyRef.key)) {
    problems.push("Enter the Secret name and key.");
  }
  if (secretKeyRef?.optional) problems.push("The Secret key can't be optional.");
  if (actorJwt) problems.push(...actorJwtProblems(actorJwt));
  return problems;
}

function actorJwtProblems({ audiences, expirationSeconds }: ActorJWT): string[] {
  const problems: string[] = [];
  if (audiences.length === 0) problems.push("Add at least one audience.");
  if (audiences.length > EGRESS_LIMITS.audiences) problems.push(`Use at most ${EGRESS_LIMITS.audiences} audiences.`);
  if (audiences.some((entry) => !entry || entry.length > EGRESS_LIMITS.audienceLength)) {
    problems.push(`Each audience needs 1 to ${EGRESS_LIMITS.audienceLength} characters.`);
  }
  if (new Set(audiences).size !== audiences.length) problems.push("Each audience must be different.");
  if (audiences.includes(RESERVED_AUDIENCE)) problems.push(`The ${RESERVED_AUDIENCE} audience is reserved.`);
  if (
    expirationSeconds !== undefined &&
    (!Number.isInteger(expirationSeconds) ||
      expirationSeconds < EGRESS_LIMITS.minExpirationSeconds ||
      expirationSeconds > EGRESS_LIMITS.maxExpirationSeconds)
  ) {
    problems.push(
      `Token lifetime must be ${EGRESS_LIMITS.minExpirationSeconds} to ${EGRESS_LIMITS.maxExpirationSeconds} seconds.`,
    );
  }
  return problems;
}

/** Spec-level problems: the destination count and duplicate origins. */
export function specProblems(spec: EgressPolicySpec): string[] {
  const problems: string[] = [];
  if (spec.destinations.length === 0) problems.push("Add at least one destination.");
  if (spec.destinations.length > EGRESS_LIMITS.destinations) {
    problems.push(`Use at most ${EGRESS_LIMITS.destinations} destinations.`);
  }
  const origins = spec.destinations.map((entry) => entry.origin).filter(Boolean);
  const repeated = origins.find((origin, index) => origins.indexOf(origin) !== index);
  if (repeated) problems.push(`${repeated} is listed twice.`);
  return problems;
}

// region Reading a policy

export type CredentialSource = "secret" | "actorJwt";

export function credentialSource(destination: EgressDestination): CredentialSource | undefined {
  if (destination.authentication?.secretKeyRef) return "secret";
  if (destination.authentication?.actorJwt) return "actorJwt";
  return undefined;
}

/** The credential sources a policy uses, in a fixed order. */
export function credentialSources(policy: EgressPolicy): CredentialSource[] {
  const used = new Set(policy.resource.spec.destinations.map(credentialSource));
  return (["secret", "actorJwt"] as const).filter((source) => used.has(source));
}

// region Used by

export type EgressUserKind = "Agent" | "SandboxTemplate";
export type EgressAppliedState = "applied" | "notApplied" | "notReported";

export interface EgressPolicyUser {
  kind: EgressUserKind;
  namespace: string;
  name: string;
  applied: EgressAppliedState;
  /** The condition's reason and message, when it is not applied. */
  detail?: string;
}

export const EGRESS_POLICIES_APPLIED = "EgressPoliciesApplied";
const EGRESS_GRANT_CONFLICT = "EgressGrantConflict";

// The controller prefixes each issue with `<namespace>/<name>: ` and joins issues with "; ".
const POLICY_ISSUE = /(?:^|; )[a-z0-9.-]+\/[a-z0-9.-]+: /;

/**
 * Reads one policy's share of the `EgressPoliciesApplied` condition, which covers all of an owner's policies.
 * A conflict holds back every grant, and an issue naming no policy cannot be attributed, so both count against this one.
 * A condition recorded for an older `generation` has not evaluated the current refs, so it is not reported yet.
 */
export function appliedState(
  conditions: readonly AgentCondition[] | undefined,
  policy: { namespace: string; name: string },
  generation?: number,
): { applied: EgressAppliedState; detail?: string } {
  const condition = conditions?.find((entry) => entry.type === EGRESS_POLICIES_APPLIED);
  if (!condition) return { applied: "notReported" };
  const stale = generation !== undefined && condition.observedGeneration !== undefined && condition.observedGeneration < generation;
  if (stale) return { applied: "notReported" };
  if (condition.status === "True") return { applied: "applied" };
  const message = condition.message ?? "";
  const tag = `${policy.namespace}/${policy.name}: `;
  const namesThis = message.startsWith(tag) || message.includes(`; ${tag}`);
  if (condition.reason !== EGRESS_GRANT_CONFLICT && !namesThis && POLICY_ISSUE.test(message)) {
    return { applied: "applied" };
  }
  return {
    applied: "notApplied",
    detail: [condition.reason, condition.message].filter(Boolean).join(": ") || undefined,
  };
}

/** Whether `refs` names the policy. Refs resolve in the owner's own namespace. */
function references(
  owner: { namespace: string; refs?: readonly { name: string }[] },
  policy: { namespace: string; name: string },
): boolean {
  return owner.namespace === policy.namespace && (owner.refs ?? []).some((ref) => ref.name === policy.name);
}

/** Every Agent and SandboxTemplate that references the policy, sorted by kind then name. */
export function policyUsers(
  policy: { namespace: string; name: string },
  agents: readonly Agent[],
  sandboxTemplates: readonly SandboxTemplate[],
): EgressPolicyUser[] {
  const users: EgressPolicyUser[] = [
    ...agents
      .filter((agent) => references({ namespace: agent.namespace, refs: agent.resource.spec.egressPolicyRefs }, policy))
      .map((agent) => ({
        kind: "Agent" as const,
        namespace: agent.namespace,
        name: agent.name,
        ...appliedState(agent.resource.status?.conditions, policy, agent.resource.metadata.generation),
      })),
    ...sandboxTemplates
      .filter((template) => references({ namespace: template.namespace, refs: template.resource.spec?.egressPolicyRefs }, policy))
      .map((template) => ({
        kind: "SandboxTemplate" as const,
        namespace: template.namespace,
        name: template.name,
        ...appliedState(template.resource.status?.conditions, policy, template.resource.metadata.generation),
      })),
  ];
  return users.sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name));
}
