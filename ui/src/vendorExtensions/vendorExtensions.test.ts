import { createElement } from "react";
import { afterEach, describe, expect, it } from "vitest";
import {
  applyVendorFieldValues,
  buildSidebarSections,
  defineVendorFormField,
  initialVendorFieldValues,
  isNavPathActive,
  installVendorApiExtension,
  readVendorFieldValues,
  validateVendorExtensionConfig,
  validateVendorFieldValues,
} from "./index";
import type { VendorExtensionConfig } from "./index";
import { apiBaseUrl, clearApiExtensions, invoke, resolveEndpoint } from "@/api";
// The two appliers are internal to the data layer — the HTTP client is their
// only production caller — so they come from the module rather than the barrel.
import {
  applyRequestTransforms,
  applyResponseTransforms,
} from "@/api/extensionPoints";
import type { ApiCallId, ApiRequestContext, ApiResponseContext } from "@/api";
import type { NavItem } from "@/components/Structure/navItems";
import { reservedRoutePaths } from "@/router/router";

// These four capabilities — endpoint resolution, payload mapping, sidebar
// ordering and config validation — have no rendered surface of their own, so
// this is where they are checked.

const noopIcon = (() => null) as unknown as NavItem["icon"];

function coreItem(key: string, order: number, path = `/${key}`): NavItem {
  return { key, label: key, path, icon: noopIcon, order };
}

function vendorItem(key: string, order: number) {
  return { key, order, Component: () => null };
}

describe("installVendorApiExtension", () => {
  afterEach(() => clearApiExtensions());

  const requestContext = (
    call: ApiCallId,
    url = `${apiBaseUrl}/kagent.api.v1alpha1.AgentService/ListAgents`,
  ): ApiRequestContext => ({ endpoint: call, method: "POST", url, headers: {} });

  const responseContext = (call: ApiCallId): ApiResponseContext => ({
    endpoint: call,
    status: 200,
    url: "/api/kagent.api.v1alpha1.AgentService/ListAgents",
  });

  it("installs nothing and undoes cleanly when there is no extension", () => {
    const undo = installVendorApiExtension(undefined);
    expect(() => undo()).not.toThrow();
  });

  // The gRPC replacement for a path override: the vendor answers the operation
  // itself, and `invoke` — which is the only way the client reaches an operation —
  // uses its implementation instead of the RPC.
  it("answers an operation from the vendor's own implementation", async () => {
    installVendorApiExtension({
      operations: { "namespaces.list": async () => [{ name: "vendor", status: "Active" }] },
    });

    await expect(invoke("namespaces.list", {})).resolves.toEqual([
      { name: "vendor", status: "Active" },
    ]);
  });

  it("undoes an operation override", async () => {
    const undo = installVendorApiExtension({
      operations: { "namespaces.list": async () => [{ name: "vendor", status: "Active" }] },
    });
    undo();

    // Back to the default implementation, so the point is only that the answer is no
    // longer the vendor's.
    //
    // Asserted as "not the vendor's answer" rather than as a rejection, because the
    // default implementation reaches the network: this spec used to require the call
    // to *fail*, which held only while nothing was listening on the dev API address.
    // With a port-forward up it succeeded and returned the cluster's real namespaces,
    // and the suite failed for a reason that had nothing to do with overrides.
    await expect(
      invoke("namespaces.list", {}).catch(() => "the default implementation failed"),
    ).resolves.not.toEqual([{ name: "vendor", status: "Active" }]);
  });

  it("points an HTTP endpoint at the vendor's path", () => {
    installVendorApiExtension({ endpoints: { "chat.a2a": "/v2/a2a" } });
    expect(resolveEndpoint("chat.a2a", { namespace: "kagent", name: "k8s" })).toBe(
      "/v2/a2a",
    );
  });

  it("undoes an endpoint override", () => {
    const undo = installVendorApiExtension({ endpoints: { "chat.a2a": "/v2/a2a" } });
    undo();
    expect(resolveEndpoint("chat.a2a", { namespace: "kagent", name: "k8s" })).toBe(
      "/a2a/kagent/k8s",
    );
  });

  it("rewrites the base URL prefix of a request", async () => {
    installVendorApiExtension({ baseUrl: "https://example.test/v1/" });
    const result = await applyRequestTransforms(requestContext("agents.list"));
    expect(result.url).toBe(
      "https://example.test/v1/kagent.api.v1alpha1.AgentService/ListAgents",
    );
  });

  it("leaves a URL that is not under the app's base alone", async () => {
    installVendorApiExtension({ baseUrl: "https://example.test" });
    const result = await applyRequestTransforms(
      requestContext("agents.list", "https://elsewhere.test/agents"),
    );
    expect(result.url).toBe("https://elsewhere.test/agents");
  });

  it("applies a per-call request transform only to its own call", async () => {
    installVendorApiExtension({
      transforms: {
        "agents.list": {
          request: (context) => ({
            ...context,
            headers: { ...context.headers, "x-example": "1" },
          }),
        },
      },
    });

    const matched = await applyRequestTransforms(requestContext("agents.list"));
    expect(matched.headers).toEqual({ "x-example": "1" });

    const other = await applyRequestTransforms(requestContext("models.list"));
    expect(other.headers).toEqual({});
  });

  it("applies a global request hook to every call", async () => {
    installVendorApiExtension({
      request: (context) => ({
        ...context,
        headers: { ...context.headers, authorization: "Bearer t" },
      }),
    });

    for (const call of ["agents.list", "models.list", "chat.a2a"] as const) {
      const result = await applyRequestTransforms(requestContext(call, "/x"));
      expect(result.headers).toEqual({ authorization: "Bearer t" });
    }
  });

  // The hook's whole value is deciding on the strength of where the request is
  // finally going, so it has to run after the base URL has been rewritten. If it
  // ran first it would see the application's own URL and could not tell a call
  // bound for the vendor's control plane from any other.
  it("runs the global hook after the base URL rewrite", async () => {
    let seen = "";
    installVendorApiExtension({
      baseUrl: "https://example.test/v1",
      request: (context) => {
        seen = context.url;
        return context;
      },
    });

    await applyRequestTransforms(requestContext("agents.list"));
    expect(seen).toBe(
      "https://example.test/v1/kagent.api.v1alpha1.AgentService/ListAgents",
    );
  });

  it("reshapes a response for its own call and no other", async () => {
    installVendorApiExtension({
      transforms: {
        "agents.list": {
          response: (body) => (body as { items: unknown }).items,
        },
      },
    });

    expect(
      await applyResponseTransforms({ items: [1, 2] }, responseContext("agents.list")),
    ).toEqual([1, 2]);
    // A different call's payload passes through untouched.
    expect(
      await applyResponseTransforms({ items: [1, 2] }, responseContext("models.list")),
    ).toEqual({ items: [1, 2] });
  });
});

describe("buildSidebarSections", () => {
  const core = [coreItem("agents", 200), coreItem("models", 300)];

  it("groups consecutive core items into one run", () => {
    const sections = buildSidebarSections(core, []);
    expect(sections).toHaveLength(1);
    expect(sections[0].kind).toBe("core");
  });

  it("splits the core run so a vendor item lands at its order", () => {
    const sections = buildSidebarSections(core, [vendorItem("example", 250)]);
    expect(sections.map((section) => section.kind)).toEqual([
      "core",
      "vendor",
      "core",
    ]);
  });

  it("puts a vendor item ordered before everything first", () => {
    const sections = buildSidebarSections(core, [vendorItem("example", 50)]);
    expect(sections.map((section) => section.kind)).toEqual(["vendor", "core"]);
  });

  it("matches nav paths by prefix, except the root", () => {
    expect(isNavPathActive("/agents", "/agents/foo/chat")).toBe(true);
    expect(isNavPathActive("/", "/agents")).toBe(false);
    expect(isNavPathActive("/", "/")).toBe(true);
  });
});

describe("form field payload mapping", () => {
  // A field whose value lives somewhere quite unlike its own id, which is the
  // case the contract exists for.
  const tierField = defineVendorFormField<string>({
    id: "tier",
    formId: "app_agents_agentNew_agentForm",
    Component: () => null,
    defaultValue: "standard",
    fromPayload: (payload) => {
      const metadata = payload.metadata as
        | { annotations?: Record<string, string> }
        | undefined;
      return metadata?.annotations?.["example/tier"] ?? "standard";
    },
    toPayload: (payload, value) => ({
      ...payload,
      metadata: { annotations: { "example/tier": value } },
    }),
    validate: (value) => (value === "" ? "Pick a tier" : undefined),
  });

  const fields = [tierField];

  it("seeds a blank form from the declared defaults", () => {
    expect(initialVendorFieldValues(fields)).toEqual({ tier: "standard" });
  });

  it("writes the value into the vendor's own payload shape", () => {
    const payload = applyVendorFieldValues(fields, { kind: "Agent" }, {
      tier: "regulated",
    });
    expect(payload).toEqual({
      kind: "Agent",
      metadata: { annotations: { "example/tier": "regulated" } },
    });
  });

  it("round-trips a value back out of a payload", () => {
    const payload = applyVendorFieldValues(fields, {}, { tier: "restricted" });
    expect(readVendorFieldValues(fields, payload)).toEqual({
      tier: "restricted",
    });
  });

  it("reports validation messages by field id", () => {
    expect(validateVendorFieldValues(fields, { tier: "" })).toEqual({
      tier: "Pick a tier",
    });
    expect(validateVendorFieldValues(fields, { tier: "standard" })).toEqual({});
  });
});

describe("validateVendorExtensionConfig", () => {
  const base: VendorExtensionConfig = { id: "example", name: "Example" };
  const coreRoutePaths = reservedRoutePaths;

  it("accepts a config that contributes nothing", () => {
    expect(() => validateVendorExtensionConfig(base)).not.toThrow();
  });

  it("rejects a slot naming an unknown extension point", () => {
    const config = {
      ...base,
      // Deliberately bypassing the typed key check, which is what a config
      // deserialised from JSON would do.
      slots: { app_agents_typo_notAPoint: () => null },
    } as unknown as VendorExtensionConfig;

    expect(() => validateVendorExtensionConfig(config)).toThrow(
      /not a known extension point/,
    );
  });

  /*
   * The reserved list is derived from the router rather than written out, and this is
   * what keeps it that way. A hardcoded list drifts the moment a core route is added:
   * the new page would answer the address, the contributed one would never render, and
   * the config that collided would still boot clean. Adding an agent details route is
   * exactly how that nearly happened.
   */
  it("reserves every core route, so a new one cannot be collided with silently", () => {
    for (const path of coreRoutePaths) {
      const config: VendorExtensionConfig = {
        ...base,
        routes: [{ path, element: createElement("div") }],
      };
      expect(
        () => validateVendorExtensionConfig(config, reservedRoutePaths),
        `a contributed route at "${path}" should be rejected without \`replaces\``,
      ).toThrow(/collides with a core route/);
    }
  });

  it("rejects a route colliding with a core one", () => {
    const config: VendorExtensionConfig = {
      ...base,
      routes: [{ path: "/agents", element: createElement("div") }],
    };
    expect(() => validateVendorExtensionConfig(config, ["/agents"])).toThrow(
      /collides with a core route/,
    );
  });

  it("rejects duplicate nav keys", () => {
    const config: VendorExtensionConfig = {
      ...base,
      navItems: [vendorItem("dup", 10), vendorItem("dup", 20)],
    };
    expect(() => validateVendorExtensionConfig(config)).toThrow(
      /declared twice/,
    );
  });

  it("reports every problem in one throw", () => {
    const config = {
      ...base,
      slots: { nope: () => null },
      navItems: [vendorItem("dup", 10), vendorItem("dup", 20)],
    } as unknown as VendorExtensionConfig;

    try {
      validateVendorExtensionConfig(config);
      expect.unreachable("should have thrown");
    } catch (error) {
      expect((error as Error).message).toMatch(/not a known extension point/);
      expect((error as Error).message).toMatch(/declared twice/);
    }
  });
});
