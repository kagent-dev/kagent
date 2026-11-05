import { describe, expect, it } from "vitest";
import type { EgressPolicy } from "@/api";
import { draftFromPolicy, draftIssues, emptyDestination, emptyPolicyDraft, resourceFromDraft } from "./egressPolicyDraft";

const policy: EgressPolicy = {
  ref: "kagent/github",
  namespace: "kagent",
  name: "github",
  resource: {
    apiVersion: "api.kagent.dev/v1alpha3",
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
});
