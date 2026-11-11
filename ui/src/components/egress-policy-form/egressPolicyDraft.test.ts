import { describe, expect, it } from "vitest";
import type { EgressPolicy } from "@/api";
import { KAGENT_API_VERSION } from "@/api/grpc/wire";
import {
  POLICY_NAME_MAX,
  draftFromPolicy,
  draftIssues,
  emptyDestination,
  emptyPolicyDraft,
  policyNameProblem,
  resourceFromDraft,
} from "./egressPolicyDraft";
import { egressCopy } from "./egressPolicyCopy";

const policy: EgressPolicy = {
  ref: "kagent/github",
  namespace: "kagent",
  name: "github",
  resource: {
    apiVersion: KAGENT_API_VERSION,
    kind: "EgressPolicy",
    metadata: { name: "github", namespace: "kagent", resourceVersion: "7", labels: { team: "infra" } },
    spec: {
      destinations: [
        { origin: "https://api.github.com", authentication: { header: "Authorization", prefix: "Bearer ", secretKeyRef: { name: "gh", key: "token" } } },
        { origin: "https://tools.example.com", authentication: { header: "Authorization", actorJwt: { audiences: ["tools"], expirationSeconds: 600 } } },
        { origin: "https://*.example.org" },
      ],
    },
  },
};

describe("egress policy draft", () => {
  it("round-trips a policy and keeps the metadata an update needs", () => {
    const resource = resourceFromDraft(draftFromPolicy(policy), policy.resource);
    expect(resource).toEqual(policy.resource);
    expect(resource.metadata.resourceVersion).toBe("7");
    expect(resource.metadata.labels).toEqual({ team: "infra" });
  });

  it("drops an empty prefix and switches credential sources cleanly", () => {
    const draft = emptyPolicyDraft("kagent");
    draft.name = "new";
    draft.destinations = [
      { ...emptyDestination(), origin: " https://api.example.com ", auth: "secret", header: "X-API-Key", secretName: "s", secretKey: "k", audiences: ["left-over"] },
    ];
    expect(resourceFromDraft(draft).spec.destinations).toEqual([
      { origin: "https://api.example.com", authentication: { header: "X-API-Key", secretKeyRef: { name: "s", key: "k" } } },
    ]);
  });

  it("reports problems per destination and for the policy", () => {
    const draft = emptyPolicyDraft("kagent");
    draft.destinations = [{ ...emptyDestination(), origin: "https://*.example.com", auth: "secret", header: "Cookie", secretName: "s", secretKey: "k" }];
    const issues = draftIssues(draft);
    expect(issues.policy).toEqual(["Enter a name."]);
    expect(issues.destinations[draft.destinations[0].id]).toEqual([
      "A wildcard origin can't use authentication.",
      "The Cookie header can't carry a credential.",
    ]);
  });

  it("accepts a dotted name on edit and sends it back unchanged", () => {
    const dotted: EgressPolicy = {
      ...policy,
      ref: "kagent/api.github.com",
      name: "api.github.com",
      resource: { ...policy.resource, metadata: { ...policy.resource.metadata, name: "api.github.com" } },
    };
    const draft = draftFromPolicy(dotted);
    expect(draftIssues(draft).count).toBe(0);
    expect(resourceFromDraft(draft, dotted.resource)).toEqual(dotted.resource);
  });

  it("refuses a namespace that is not a single DNS label", () => {
    const draft = emptyPolicyDraft("team.a");
    draft.name = "github";
    draft.destinations = [{ ...emptyDestination(), origin: "https://api.github.com" }];
    expect(draftIssues(draft).policy).toEqual([egressCopy.form.namespaceInvalid]);
  });
});

describe("policyNameProblem", () => {
  it.each([
    ["a single label", "github"],
    ["dotted labels", "api.github.com"],
    ["hyphens inside labels", "my-team.api-v2"],
    ["the length limit", "a".repeat(POLICY_NAME_MAX)],
  ])("accepts %s", (_case, name) => {
    expect(policyNameProblem(name)).toBeUndefined();
  });

  it.each([
    ["consecutive dots", "api..github"],
    ["a leading dot", ".github"],
    ["a trailing dot", "github."],
    ["a leading hyphen", "-github"],
    ["a trailing hyphen", "github-"],
    ["a hyphen at a dot", "api-.github"],
    ["uppercase", "GitHub"],
  ])("refuses %s", (_case, name) => {
    expect(policyNameProblem(name)).toBe(egressCopy.form.nameInvalid);
  });

  it("refuses a name past the length limit", () => {
    expect(policyNameProblem("a".repeat(POLICY_NAME_MAX + 1))).toBe(egressCopy.form.nameTooLong(POLICY_NAME_MAX));
  });

  it("asks for a name when it is blank", () => {
    expect(policyNameProblem("  ")).toBe(egressCopy.form.nameRequired);
  });
});
