import { describe, expect, it } from "vitest";
import type { Agent } from "./agents";
import type { SandboxTemplate } from "./sandboxTemplates";
import {
  appliedState,
  destinationProblems,
  policyUsers,
  specProblems,
  type EgressAuthentication,
  type EgressDestination,
} from "./egressPolicies";

const secret = { name: "auth", key: "token" };
const bearer: EgressAuthentication = { header: "Authorization", prefix: "Bearer ", secretKeyRef: secret };

// The same cases as `go/api/v1alpha3/egresspolicy_cel_test.go`, so the form and the CRD agree.
describe("destinationProblems", () => {
  it.each<[string, EgressDestination, boolean]>([
    ["HTTPS", { origin: "https://api.example.com" }, false],
    ["HTTP", { origin: "http://registry.example.com:8080" }, false],
    ["wildcard", { origin: "https://*.example.com" }, false],
    ["bearer", { origin: "https://api.example.com:8443", authentication: bearer }, false],
    ["API key", { origin: "https://api.example.com", authentication: { header: "X-API-Key", secretKeyRef: secret } }, false],
    ["empty prefix", { origin: "https://api.example.com", authentication: { header: "X-API-Key", prefix: "", secretKeyRef: secret } }, false],
    ["HTTP auth", { origin: "http://api.example.com", authentication: bearer }, false],
    ["actor JWT", { origin: "https://api.example.com", authentication: { header: "Authorization", actorJwt: { audiences: ["api"] } } }, false],
    ["HTTP wildcard auth", { origin: "http://*.example.com", authentication: bearer }, true],
    ["wildcard auth", { origin: "https://*.example.com", authentication: bearer }, true],
    ["empty auth", { origin: "https://api.example.com", authentication: { header: "" } }, true],
    ["missing header", { origin: "https://api.example.com", authentication: { header: "", secretKeyRef: secret } }, true],
    ["both credentials", { origin: "https://api.example.com", authentication: { ...bearer, actorJwt: { audiences: ["api"] } } }, true],
    ["path", { origin: "https://api.example.com/v1" }, true],
    ["userinfo", { origin: "https://user@api.example.com" }, true],
    ["query", { origin: "https://api.example.com?" }, true],
    ["IP", { origin: "https://192.0.2.1" }, true],
    ["all hosts", { origin: "https://*" }, true],
    ["top level wildcard", { origin: "https://*.com" }, true],
    ["interior wildcard", { origin: "https://api.*.com" }, true],
    ["port zero", { origin: "https://api.example.com:0" }, true],
    ["port overflow", { origin: "https://api.example.com:65536" }, true],
  ])("%s", (_name, destination, reject) => {
    expect(destinationProblems(destination).length > 0).toBe(reject);
  });

  it.each(["Host", "cOnNeCtIoN", "Content-Length", "Transfer-Encoding", "Cookie", "Proxy-Authorization", "X-Key\r\nInjected"])(
    "refuses the %j header",
    (header) => {
      expect(destinationProblems({ origin: "https://api.example.com", authentication: { header, secretKeyRef: secret } })).not.toEqual([]);
    },
  );

  it.each(["Bearer\r", "Bearer\n", "\x00", "\x08", "\x0b", "\x1f", "\x7f", "x".repeat(257)])("refuses the prefix %j", (prefix) => {
    expect(destinationProblems({ origin: "https://api.example.com", authentication: { ...bearer, prefix } })).not.toEqual([]);
  });

  it("allows a tab in the prefix", () => {
    expect(destinationProblems({ origin: "https://api.example.com", authentication: { ...bearer, prefix: "Bearer\t" } })).toEqual([]);
  });

  it.each([{ name: "", key: "token" }, { name: "auth", key: "" }, { name: "auth", key: "token", optional: true }])(
    "refuses the Secret key %j",
    (secretKeyRef) => {
      expect(destinationProblems({ origin: "https://api.example.com", authentication: { header: "Authorization", secretKeyRef } })).not.toEqual([]);
    },
  );

  it.each([
    ["no audiences", { audiences: [] }],
    ["the reserved audience", { audiences: ["kagent-runtime"] }],
    ["a repeated audience", { audiences: ["api", "api"] }],
    ["a short lifetime", { audiences: ["api"], expirationSeconds: 299 }],
    ["a long lifetime", { audiences: ["api"], expirationSeconds: 3601 }],
  ])("refuses an Actor JWT with %s", (_name, actorJwt) => {
    expect(destinationProblems({ origin: "https://api.example.com", authentication: { header: "Authorization", actorJwt } })).not.toEqual([]);
  });
});

describe("specProblems", () => {
  it("needs between 1 and 64 destinations with distinct origins", () => {
    const origin = (n: number) => ({ origin: `https://h${n}.example.com` });
    expect(specProblems({ destinations: [] })).not.toEqual([]);
    expect(specProblems({ destinations: Array.from({ length: 64 }, (_, n) => origin(n)) })).toEqual([]);
    expect(specProblems({ destinations: Array.from({ length: 65 }, (_, n) => origin(n)) })).not.toEqual([]);
    expect(specProblems({ destinations: [origin(1), origin(1)] })).toEqual(["https://h1.example.com is listed twice."]);
  });
});

describe("policyUsers", () => {
  const agent = (namespace: string, name: string, refs: string[], conditions?: Agent["resource"]["status"]): Agent => ({
    ref: `${namespace}/${name}`,
    namespace,
    name,
    resource: {
      metadata: { name, namespace },
      spec: { template: { ref: { name: "t" } }, harnessRef: { name: "h" }, egressPolicyRefs: refs.map((ref) => ({ name: ref })) },
      status: conditions,
    },
  });
  const sandbox = (namespace: string, name: string, refs: string[] | undefined): SandboxTemplate => ({
    ref: `${namespace}/${name}`,
    namespace,
    name,
    resource: { metadata: { name, namespace }, spec: refs ? { egressPolicyRefs: refs.map((ref) => ({ name: ref })) } : undefined },
  });

  it("matches on namespace and name, since refs resolve in the owner's namespace", () => {
    const users = policyUsers(
      { namespace: "kagent", name: "github" },
      [
        agent("kagent", "zeta", ["github"], {
          conditions: [{ type: "EgressPoliciesApplied", status: "True", reason: "Applied" }],
        }),
        agent("kagent", "alpha", ["github", "other"], {
          conditions: [{ type: "EgressPoliciesApplied", status: "False", reason: "EgressPolicyNotFound", message: "other not found" }],
        }),
        agent("other-team", "same-name", ["github"]),
        agent("kagent", "unrelated", ["gitlab"]),
      ],
      [sandbox("kagent", "box", ["github"]), sandbox("kagent", "bare", undefined), sandbox("platform", "box", ["github"])],
    );

    expect(users).toEqual([
      { kind: "Agent", namespace: "kagent", name: "alpha", applied: "notApplied", detail: "EgressPolicyNotFound: other not found" },
      { kind: "Agent", namespace: "kagent", name: "zeta", applied: "applied" },
      { kind: "SandboxTemplate", namespace: "kagent", name: "box", applied: "notReported" },
    ]);
  });

  it("reports a missing condition as not reported rather than applied", () => {
    expect(appliedState(undefined)).toEqual({ applied: "notReported" });
    expect(appliedState([{ type: "Ready", status: "True" }])).toEqual({ applied: "notReported" });
  });
});
