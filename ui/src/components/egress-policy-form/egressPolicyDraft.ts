import {
  destinationProblems,
  specProblems,
  type EgressDestination,
  type EgressPolicy,
  type EgressPolicyResource,
} from "@/api/domain/egressPolicies";
import { KAGENT_API_VERSION } from "@/api/grpc/wire";
import { DEFAULT_NAMESPACE, isValidResourceName } from "@/components/common/resourceName";

export type AuthChoice = "none" | "secret" | "actorJwt";

/** One destination as the form edits it: every field a string, auth as a choice. */
export interface DestinationDraft {
  id: number;
  origin: string;
  auth: AuthChoice;
  header: string;
  prefix: string;
  secretName: string;
  secretKey: string;
  audiences: string[];
  expirationSeconds: string;
}

export interface EgressPolicyDraft {
  name: string;
  namespace: string;
  destinations: DestinationDraft[];
}

let nextId = 1;

export function emptyDestination(): DestinationDraft {
  return {
    id: nextId++,
    origin: "",
    auth: "none",
    header: "",
    prefix: "",
    secretName: "",
    secretKey: "",
    audiences: [],
    expirationSeconds: "",
  };
}

export function emptyPolicyDraft(namespace = DEFAULT_NAMESPACE): EgressPolicyDraft {
  return { name: "", namespace, destinations: [emptyDestination()] };
}

export function draftFromPolicy(policy: EgressPolicy): EgressPolicyDraft {
  return {
    name: policy.name,
    namespace: policy.namespace,
    destinations: policy.resource.spec.destinations.map(({ origin, authentication: auth }) => ({
      ...emptyDestination(),
      origin,
      auth: auth?.secretKeyRef ? "secret" : auth?.actorJwt ? "actorJwt" : "none",
      header: auth?.header ?? "",
      prefix: auth?.prefix ?? "",
      secretName: auth?.secretKeyRef?.name ?? "",
      secretKey: auth?.secretKeyRef?.key ?? "",
      audiences: auth?.actorJwt?.audiences ?? [],
      expirationSeconds: auth?.actorJwt?.expirationSeconds?.toString() ?? "",
    })),
  };
}

/** The CRD destination a draft row stands for. Origins and headers are trimmed. */
export function destinationFromDraft(row: DestinationDraft): EgressDestination {
  const origin = row.origin.trim();
  if (row.auth === "none") return { origin };
  const seconds = row.expirationSeconds.trim();
  return {
    origin,
    authentication: {
      header: row.header.trim(),
      // Omitted and empty mean the same thing to the CRD.
      ...(row.prefix ? { prefix: row.prefix } : {}),
      ...(row.auth === "secret"
        ? { secretKeyRef: { name: row.secretName.trim(), key: row.secretKey.trim() } }
        : {
            actorJwt: {
              audiences: row.audiences.map((entry) => entry.trim()),
              ...(seconds ? { expirationSeconds: Number(seconds) } : {}),
            },
          }),
    },
  };
}

/**
 * The resource to send. An edit keeps the loaded metadata, so `resourceVersion`
 * guards against overwriting someone else's change and the labels survive.
 */
export function resourceFromDraft(
  draft: EgressPolicyDraft,
  existing?: EgressPolicyResource,
): EgressPolicyResource {
  const name = draft.name.trim();
  const namespace = draft.namespace.trim();
  return {
    apiVersion: KAGENT_API_VERSION,
    kind: "EgressPolicy",
    metadata: { ...existing?.metadata, name, namespace },
    spec: { destinations: draft.destinations.map(destinationFromDraft) },
  };
}

export interface DraftIssues {
  /** Name, namespace and spec-level problems. */
  policy: string[];
  /** Problems per destination row, by row id. */
  destinations: Record<number, string[]>;
  count: number;
}

export function draftIssues(draft: EgressPolicyDraft): DraftIssues {
  const policy: string[] = [];
  if (!draft.name.trim()) policy.push("Enter a name.");
  else if (!isValidResourceName(draft.name) || draft.name.trim().length > 253) {
    policy.push("Use lowercase letters, numbers and hyphens in the name.");
  }
  if (!draft.namespace.trim()) policy.push("Choose a namespace.");
  const spec = { destinations: draft.destinations.map(destinationFromDraft) };
  policy.push(...specProblems(spec));

  const destinations: Record<number, string[]> = {};
  let count = policy.length;
  draft.destinations.forEach((row, index) => {
    const problems = destinationProblems(spec.destinations[index]);
    if (problems.length > 0) {
      destinations[row.id] = problems;
      count += problems.length;
    }
  });
  return { policy, destinations, count };
}
