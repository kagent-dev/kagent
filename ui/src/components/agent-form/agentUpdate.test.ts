import { describe, expect, it } from "vitest";
import { agentUpdatePayload } from "./agentUpdate";
import type { Agent, AgentCreateRequest } from "@/api";

/**
 * `agents.update` replaces the whole resource, so this is the difference between an edit
 * and an accidental deletion. The cases below are the ones that actually went wrong, or
 * would have if the merge were written either of the two obvious lazy ways.
 */

const existing = {
  apiVersion: "kagent.dev/v1alpha2",
  kind: "Agent",
  metadata: {
    name: "k8s-agent",
    namespace: "kagent",
    creationTimestamp: "2026-01-01T00:00:00Z",
    resourceVersion: "48213",
    labels: { "app.kubernetes.io/managed-by": "Helm", team: "platform" },
    annotations: { "argocd.argoproj.io/tracking-id": "agents:kagent.dev/Agent:kagent/k8s-agent" },
  },
  spec: {
    type: "Declarative",
    description: "Reads a cluster",
    declarative: {
      modelConfig: "default",
      systemMessage: "old instructions",
      a2aConfig: { skills: [{ id: "diagnose", name: "Diagnose" }] },
    },
    // Not modelled by the form at the time of writing: the case this exists for.
    imagePullSecrets: [{ name: "registry" }],
  },
} as unknown as Agent;

const edited = {
  metadata: { name: "k8s-agent", namespace: "kagent" },
  spec: {
    type: "Declarative",
    description: "Reads a cluster, carefully",
    declarative: { modelConfig: "default", systemMessage: "new instructions" },
  },
} as unknown as AgentCreateRequest;

describe("agentUpdatePayload", () => {
  it("keeps the labels and annotations the form never showed", () => {
    const payload = agentUpdatePayload(existing, edited);

    expect(payload.metadata.labels).toEqual({
      "app.kubernetes.io/managed-by": "Helm",
      team: "platform",
    });
    expect(payload.metadata.annotations?.["argocd.argoproj.io/tracking-id"]).toContain(
      "k8s-agent",
    );
  });

  it("takes the edited values where the form owns the field", () => {
    const payload = agentUpdatePayload(existing, edited);

    expect(payload.spec.description).toBe("Reads a cluster, carefully");
    expect(payload.spec.declarative?.systemMessage).toBe("new instructions");
  });

  // The form owns `declarative` whole, so an edit that drops `a2aConfig` — which is what
  // removing every skill does — has to remove it, not inherit the old skills back.
  it("lets the form clear part of a field it owns", () => {
    const payload = agentUpdatePayload(existing, edited);

    expect(payload.spec.declarative?.a2aConfig).toBeUndefined();
  });

  it("carries through a spec field the form does not model", () => {
    const payload = agentUpdatePayload(existing, edited);

    expect(
      (payload.spec as unknown as { imagePullSecrets?: unknown }).imagePullSecrets,
    ).toEqual([{ name: "registry" }]);
  });

  /*
   * The CRD has thirteen fields under `declarative` and the form models nine, so treating
   * the block as owned deleted the other four from any agent that used them. These are
   * the ones a cluster actually sets.
   */
  it("keeps a declarative field the form does not model", () => {
    const withExtras = {
      ...existing,
      spec: {
        ...existing.spec,
        declarative: {
          ...existing.spec.declarative,
          promptTemplate: "library/greeting",
          executeCodeBlocks: true,
          systemMessageFrom: { configMapKeyRef: { name: "prompts", key: "system" } },
        },
      },
    } as unknown as Agent;

    const declarative = agentUpdatePayload(withExtras, edited).spec
      .declarative as unknown as Record<string, unknown> | undefined;

    expect(declarative?.promptTemplate).toBe("library/greeting");
    expect(declarative?.executeCodeBlocks).toBe(true);
    expect(declarative?.systemMessageFrom).toBeDefined();
    // And still takes the edit for the fields it does model.
    expect(declarative?.systemMessage).toBe("new instructions");
  });

  /*
   * `deployment` is the sharpest case: the form sets `serviceAccountName` and the CRD has
   * eighteen other fields there. An edit that emits no deployment at all — which is what
   * an empty service account produces — must not take the replicas with it.
   */
  it("keeps the rest of a deployment block the form barely touches", () => {
    const withDeployment = {
      ...existing,
      spec: {
        ...existing.spec,
        declarative: {
          ...existing.spec.declarative,
          deployment: {
            replicas: 3,
            serviceAccountName: "reader",
            env: [{ name: "LOG_LEVEL", value: "debug" }],
            resources: { limits: { cpu: "500m" } },
          },
        },
      },
    } as unknown as Agent;

    const deployment = (
      agentUpdatePayload(withDeployment, edited).spec.declarative as unknown as Record<string, unknown>
    ).deployment as Record<string, unknown>;

    expect(deployment.replicas).toBe(3);
    expect(deployment.env).toEqual([{ name: "LOG_LEVEL", value: "debug" }]);
    expect(deployment.resources).toEqual({ limits: { cpu: "500m" } });
    // The one field the form owns there is cleared, because this edit omits it.
    expect(deployment.serviceAccountName).toBeUndefined();
  });

  it("still lets the form set the service account it owns", () => {
    const withDeployment = {
      ...existing,
      spec: {
        ...existing.spec,
        declarative: { ...existing.spec.declarative, deployment: { replicas: 3 } },
      },
    } as unknown as Agent;

    const editedWithAccount = {
      ...edited,
      spec: {
        ...edited.spec,
        declarative: { ...edited.spec.declarative, deployment: { serviceAccountName: "writer" } },
      },
    } as unknown as AgentCreateRequest;

    const deployment = (
      agentUpdatePayload(withDeployment, editedWithAccount).spec
        .declarative as unknown as Record<string, unknown>
    ).deployment as Record<string, unknown>;

    expect(deployment.serviceAccountName).toBe("writer");
    expect(deployment.replicas).toBe(3);
  });

  // An empty block is not what "unset" looks like, and leaving `deployment: {}` on the
  // resource is noise the reader has to interpret.
  it("removes a block the merge leaves empty", () => {
    const onlyServiceAccount = {
      ...existing,
      spec: {
        ...existing.spec,
        declarative: {
          ...existing.spec.declarative,
          deployment: { serviceAccountName: "reader" },
        },
      },
    } as unknown as Agent;

    const declarative = agentUpdatePayload(onlyServiceAccount, edited).spec
      .declarative as unknown as Record<string, unknown>;

    expect(declarative.deployment).toBeUndefined();
  });

  it("drops the two fields the cluster assigns", () => {
    const payload = agentUpdatePayload(existing, edited);

    expect(payload.metadata.creationTimestamp).toBeUndefined();
    expect(payload.metadata.resourceVersion).toBeUndefined();
  });

  it("keeps the api version the agent was stored under", () => {
    expect(agentUpdatePayload(existing, edited).apiVersion).toBe("kagent.dev/v1alpha2");
  });

  /*
   * The exception to "keep what the form cannot see": a kind change. An agent becoming
   * BYO should not keep a declarative block, however much of it the form never modelled —
   * the block belongs to the kind it no longer is.
   */
  it("drops the whole declarative block when the kind changes", () => {
    const withExtras = {
      ...existing,
      spec: {
        ...existing.spec,
        declarative: { ...existing.spec.declarative, promptTemplate: "library/greeting" },
      },
    } as unknown as Agent;

    const becomesByo = {
      metadata: { name: "k8s-agent", namespace: "kagent" },
      spec: {
        type: "BYO",
        description: "Now a container",
        byo: { deployment: { image: "example/agent:1" } },
      },
    } as unknown as AgentCreateRequest;

    const payload = agentUpdatePayload(withExtras, becomesByo);

    expect(payload.spec.declarative).toBeUndefined();
    expect((payload.spec as unknown as { byo?: unknown }).byo).toEqual({
      deployment: { image: "example/agent:1" },
    });
    // Metadata is not the kind's, so it survives either way.
    expect(payload.metadata.labels?.team).toBe("platform");
  });

  // A form that switches an agent's kind must be able to remove the other kind's block,
  // or a declarative agent turned BYO would arrive claiming to be both.
  it("removes the block for a kind the edit no longer has", () => {
    const wasByo = {
      ...existing,
      spec: { ...existing.spec, byo: { deployment: { image: "old" } } },
    } as unknown as Agent;

    const payload = agentUpdatePayload(wasByo, edited);

    expect((payload.spec as unknown as { byo?: unknown }).byo).toBeUndefined();
  });
});
