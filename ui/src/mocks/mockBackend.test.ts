/**
 * The fixture backend, exercised through the same entry point the app uses.
 *
 * `api/operations.test.ts` proves the *client* against in-process services. This
 * proves the *fixtures*: that every operation the app can invoke is actually
 * served, that the three scenario axes still work, and that a write can be read
 * back. Those are different failures — a fake that is missing an RPC answers
 * `Unimplemented`, and nothing notices until someone opens the page it belongs to.
 * That is exactly the gap the two deleted REST-path suites existed to close, and
 * it did not go away when the paths did.
 *
 * The scenario is read from the URL here exactly as it is in a browser, so these
 * tests drive it the same way a person does.
 */

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { ApiError } from "@/api/ApiError";
import { clearApiExtensions } from "@/api/extensionPoints";
import { invoke, operationIds } from "@/api/operations";
import type { OperationId, OperationInput } from "@/api/operations";
import { setApiTransport } from "@/api/transport";
import { mockTransport } from "./transport";
import { MOCK_INSTANCE_CREATOR } from "./fixtures";

beforeAll(() => setApiTransport(mockTransport));
afterAll(() => setApiTransport(undefined));

/** The scenario, set the way a person sets it: in the URL. */
function setScenario(scenario: "ok" | "empty" | "error"): void {
  window.localStorage.clear();
  window.history.replaceState({}, "", `/?mock=${scenario}`);
}

beforeEach(() => setScenario("ok"));
afterEach(() => clearApiExtensions());

/** An agent draft, in the shape a form produces. */
const agentDraft = (name: string) => ({
  apiVersion: "kagent.dev/v1alpha3",
  kind: "SandboxAgent",
  metadata: { name, namespace: "kagent" },
  spec: {
    type: "Declarative" as const,
    description: `the ${name} agent`,
    declarative: {
      systemMessage: "be useful",
      modelConfig: "default-model-config",
      tools: [],
    },
  },
});

/**
 * A plausible input for every operation.
 *
 * `satisfies` is doing real work: an operation added to `OperationMap` without an
 * entry here fails to compile, so the sweep below cannot silently stop covering
 * the whole surface.
 */
const INPUTS = {
  "agents.list": {},
  "agents.get": { namespace: "kagent", name: "k8s-agent" },
  "agents.create": { resource: agentDraft("swept-agent") },
  "agents.update": { resource: agentDraft("swept-agent") },
  "agents.delete": { namespace: "kagent", name: "swept-agent" },

  "models.list": {},
  "models.get": { namespace: "kagent", name: "default-model-config" },
  "models.create": {
    payload: { ref: "kagent/swept-model", spec: { model: "gpt-4.1", provider: "OpenAI" } },
  },
  "models.update": {
    namespace: "kagent",
    name: "swept-model",
    payload: { ref: "kagent/swept-model", spec: { model: "gpt-4.1", provider: "OpenAI" } },
  },
  "models.delete": { namespace: "kagent", name: "swept-model" },
  "models.providers": {},
  "models.providerModels": {},

  "mcpServers.list": {},
  "mcpServers.create": {
    payload: {
      type: "RemoteMCPServer" as const,
      remoteMCPServer: {
        metadata: { name: "swept-server", namespace: "kagent" },
        spec: {
          description: "a swept server",
          protocol: "STREAMABLE_HTTP" as const,
          url: "https://example.test/mcp",
          headersFrom: [],
        },
      },
    },
  },
  "mcpServers.delete": { namespace: "kagent", name: "swept-server" },
  "tools.list": {},

  "prompts.list": {},
  "prompts.get": { namespace: "kagent", name: "shared-fragments" },
  "prompts.create": {
    payload: { namespace: "kagent", name: "swept-prompts", data: { tone: "brisk" } },
  },
  "prompts.update": {
    namespace: "kagent",
    name: "swept-prompts",
    payload: { data: { tone: "brisker" } },
  },
  "prompts.delete": { namespace: "kagent", name: "swept-prompts" },

  /*
   * Each of these acts on a different instance, because this sweep runs every
   * operation concurrently and the controller refuses a second lifecycle operation
   * on an instance that already has one in flight. Suspending and resuming the same
   * row here would be a race with itself.
   */
  "agentInstances.list": { namespace: "kagent" },
  "agentInstances.get": {
    namespace: "kagent",
    id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
  },
  "agentInstances.suspend": {
    namespace: "analytics",
    id: "5a3c8e17-4b92-4d05-9f61-8c2e7a03b4d9",
  },
  "agentInstances.resume": {
    namespace: "kagent",
    id: "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05",
  },
  "agentInstances.shares.list": {
    namespace: "kagent",
    id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
  },
  "agentInstances.shares.create": {
    namespace: "kagent",
    id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
    permission: "readOnly",
  },
  // The seeded share, not one the sweep created: the sweep runs everything at once,
  // so revoking the create above would be a race with it.
  "agentInstances.shares.revoke": {
    namespace: "kagent",
    shareId: "mock-instance-share-seed",
  },
  "agentInstances.create": {
    namespace: "kagent",
    harness: "k8s-agent",
    agentTemplate: "k8s-agent-7f3a91c",
    // Required by the controller, and by the fixture backend for the same reason.
    requestId: "swept-create",
  },
  // A different instance again, for the reason above: this sweep runs everything at
  // once, and deleting one another operation is reading would be a race. This one is
  // touched by nothing else here.
  "agentInstances.delete": {
    namespace: "kagent",
    // The scratch instance, which exists for this. It has to be one the mock caller
    // *created*: deleting is scoped to the creator exactly as reading is, so the
    // barely-written record this used to name — whose creator is nobody — now
    // answers NotFound, which is the controller's behaviour and not a fixture bug.
    id: "9c3b7e18-40d6-4a52-8b71-e2f05c96a3d7",
  },

  "harnesses.list": {},
  "harnesses.create": {
    namespace: "kagent",
    name: "made-up",
    resource: {
      metadata: { name: "made-up", namespace: "kagent" },
      spec: {
        kagent: {},
        workload: {
          image: `ghcr.io/example/runtime@sha256:${"a".repeat(64)}`,
        },
        substrate: {
          workerPoolRef: { name: "kagent-default" },
          snapshotPolicy: { location: "gs://snapshots/kagent/" },
        },
      },
    },
  },
  "harnesses.delete": { namespace: "kagent", name: "made-up" },
  "agentTemplates.list": {},
  "agentTemplates.get": { namespace: "kagent", name: "k8s-agent-7f3a91c" },
  "agentTemplates.create": {
    namespace: "kagent",
    name: "swept-template",
    resource: {
      metadata: { name: "swept-template", namespace: "kagent" },
      spec: { modelConfig: { name: "default-model-config" } },
    },
  },
  "agentTemplates.update": {
    namespace: "kagent",
    name: "note-taker",
    resource: {
      metadata: { name: "note-taker", namespace: "kagent" },
      spec: { modelConfig: { name: "default-model-config" } },
    },
  },
  // A different template again: this sweep runs everything at once, and deleting
  // one another operation is reading would be a race.
  "agentTemplates.delete": { namespace: "kagent", name: "support-triage-2b91d0e" },

  "agentInstances.rename": {
    namespace: "kagent",
    id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
    // A real name rather than an empty one: an empty name is valid and would prove
    // only that the call is wired, where this also proves the validation accepts
    // something a reader would type.
    name: "Renamed by the fixture suite",
  },

  "namespaces.list": {},
  "substrate.status": {},
  "substrate.summary": {},
  "substrate.actors": {},
  "substrate.workers": {},
} satisfies { [K in OperationId]: OperationInput<K> };

/** Runs one operation with the input above. */
function run(id: OperationId): Promise<unknown> {
  return invoke(id, INPUTS[id] as never);
}

describe("the fixture backend", () => {
  /*
   * Concurrently, because every call waits out the scenario's delay: serially this
   * would be one delay per operation for no extra coverage.
   */
  it("serves every operation the app can invoke", async () => {
    const failures = await Promise.all(
      operationIds.map(async (id) => {
        try {
          await run(id);
          return null;
        } catch (error) {
          return `${id}: ${(error as Error).message}`;
        }
      }),
    );

    expect(failures.filter(Boolean)).toEqual([]);
  });

  it("reads a created agent back, and stops listing a deleted one", async () => {
    await invoke("agents.create", { resource: agentDraft("written-agent") });

    const listed = await invoke("agents.list", {});
    expect(listed.map((row) => row.agent.metadata.name)).toContain("written-agent");

    // Resolved from the referenced ModelConfig, the way the controller resolves it:
    // a new row that left it blank would read differently from every other row.
    const created = listed.find((row) => row.agent.metadata.name === "written-agent");
    expect(created?.model).toBe("gpt-4.1");

    const read = await invoke("agents.get", {
      namespace: "kagent",
      name: "written-agent",
    });
    expect(read.agent.spec.description).toBe("the written-agent agent");

    await invoke("agents.delete", { namespace: "kagent", name: "written-agent" });
    const after = await invoke("agents.list", {});
    expect(after.map((row) => row.agent.metadata.name)).not.toContain("written-agent");
  });

  it("finds a harness when the caller does not know which kind it is", async () => {
    // `agents.get` asks for a sandbox agent first; the fake must answer that with a
    // 404 for a harness, or the fallback never runs and the harness is unreachable.
    const harness = await invoke("agents.get", {
      namespace: "analytics",
      name: "reporting-agent",
    });
    expect(harness.agentKind).toBe("AgentHarness");
  });

  /*
   * `models.providers` is two RPCs merged, and a merge with nothing on one side is
   * wired rather than exercised — so the fixtures carry one provider of each kind and
   * this asserts both arrive with the right provenance.
   */
  it("merges the providers an operator added with the ones the controller ships", async () => {
    const providers = await invoke("models.providers", {});

    const stock = providers.filter((provider) => provider.source === "stock");
    const configured = providers.filter((provider) => provider.source === "configured");
    expect(stock.length).toBeGreaterThan(0);
    expect(configured).toHaveLength(1);

    // A configured provider is a `ModelProviderConfig` resource, so its name is the
    // resource's name while its type is the provider enum. They differ, where for a
    // stock provider they are the same string — which is exactly what a picker keyed
    // on the wrong one of the two gets wrong.
    expect(configured[0].name).not.toBe(configured[0].type);
    expect(configured[0].endpoint).toBeTruthy();
    // No parameter lists: the controller reports none for a configured provider, and
    // every caller iterates them, so they are empty rather than absent.
    expect(configured[0].requiredParams).toEqual([]);
    expect(configured[0].optionalParams).toEqual([]);

    // The stock RPC must not also report it, or the picker shows it twice.
    expect(stock.map((provider) => provider.name)).not.toContain(configured[0].name);
  });

  it("lists only the prompt libraries in the namespace asked about", async () => {
    const scoped = await invoke("prompts.list", { namespace: "platform" });
    expect(scoped.map((row) => row.name)).toEqual(["incident-playbooks"]);

    const all = await invoke("prompts.list", {});
    expect(all.length).toBeGreaterThan(scoped.length);
  });

  /*
   * The fixture backend has to refuse a lifecycle operation for the same reasons the
   * controller does, or the disabled buttons on the instances page are decoration:
   * a fake that suspended anything from any state would let the page ship with its
   * preconditions inverted and nothing would object until a cluster did.
   */
  describe("agent instance lifecycle", () => {
    const READY = "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44";
    const FAILED = "d4b02f87-3a55-4c18-9e6b-1f70c9a8e332";
    const MID_OPERATION = "0a7d6c58-9e21-4b3c-a05d-4e8f1b6d2277";

    it("records a suspend, so the list and the record agree afterwards", async () => {
      const before = await invoke("agentInstances.get", {
        namespace: "kagent",
        id: READY,
      });
      expect(before.state).toBe("ready");

      const suspended = await invoke("agentInstances.suspend", {
        namespace: "kagent",
        id: READY,
      });
      expect(suspended.state).toBe("suspended");
      // Cleared, because the controller's operation completes synchronously — an
      // instance still claiming to be suspending would refuse the resume that
      // follows.
      expect(suspended.operation).toBe("unspecified");

      const listed = await invoke("agentInstances.list", { namespace: "kagent" });
      expect(listed.find((row) => row.id === READY)?.state).toBe("suspended");

      const resumed = await invoke("agentInstances.resume", {
        namespace: "kagent",
        id: READY,
      });
      expect(resumed.state).toBe("ready");
    });

    it("refuses a suspend from a state the controller would refuse", async () => {
      const error = await invoke("agentInstances.suspend", {
        namespace: "kagent",
        id: FAILED,
      }).catch((reason: unknown) => reason);

      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).code).toBe("Aborted");
    });

    it("refuses a second operation while one is already in flight", async () => {
      const error = await invoke("agentInstances.resume", {
        namespace: "kagent",
        id: MID_OPERATION,
      }).catch((reason: unknown) => reason);

      expect((error as ApiError).code).toBe("Aborted");
      expect((error as ApiError).message).toMatch(/conflicting lifecycle operation/);
    });

    /*
     * The namespace is part of an instance's address, not a filter over a larger
     * list — `validateNamespace` on the controller rejects an empty one outright.
     * A fake that treated it as "everything" would hide a page that forgot to pass
     * one until the page met a cluster.
     */
    it("will not list instances without a namespace", async () => {
      const error = await invoke("agentInstances.list", { namespace: "" }).catch(
        (reason: unknown) => reason,
      );
      expect((error as ApiError).code).toBe("InvalidArgument");
    });

    it("lists other people's instances only when asked", async () => {
      const mine = await invoke("agentInstances.list", { namespace: "kagent" });
      const everyone = await invoke("agentInstances.list", {
        namespace: "kagent",
        allCreators: true,
      });

      expect(everyone.length).toBeGreaterThan(mine.length);
      expect(mine.every((row) => row.creator === MOCK_INSTANCE_CREATOR)).toBe(true);
      expect(everyone.some((row) => row.creator !== MOCK_INSTANCE_CREATOR)).toBe(true);
    });

    it("keeps each namespace to itself", async () => {
      const analytics = await invoke("agentInstances.list", {
        namespace: "analytics",
      });
      expect(analytics.length).toBeGreaterThan(0);
      expect(analytics.every((row) => row.namespace === "analytics")).toBe(true);
    });
  });

  describe("?mock=empty", () => {
    beforeEach(() => setScenario("empty"));

    it("empties the lists", async () => {
      expect(await invoke("agents.list", {})).toEqual([]);
      expect(await invoke("models.list", {})).toEqual([]);
      expect(await invoke("namespaces.list", {})).toEqual([]);
    });

    it("answers a single resource with a 404, which is the state a page renders", async () => {
      await expect(
        invoke("agents.get", { namespace: "kagent", name: "k8s-agent" }),
      ).rejects.toMatchObject({ status: 404 });
    });
  });

  describe("?mock=error", () => {
    beforeEach(() => setScenario("error"));

    it("fails a read as the API would, and says it was asked to", async () => {
      const error = await invoke("agents.list", {}).catch((reason: unknown) => reason);

      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).status).toBe(500);
      expect((error as ApiError).message).toContain("asked to fail");
      // Named so a failing screenshot says which call broke.
      expect((error as ApiError).message).toContain("AgentService/ListAgents");
    });

    it("fails a write too, so a form's failure path is reachable", async () => {
      await expect(
        invoke("agents.create", { resource: agentDraft("never-created") }),
      ).rejects.toBeInstanceOf(ApiError);

      setScenario("ok");
      const listed = await invoke("agents.list", {});
      expect(listed.map((row) => row.agent.metadata.name)).not.toContain(
        "never-created",
      );
    });
  });
});
