import { createElement } from "react";
import { afterEach, describe, expect, it } from "vitest";
import {
  applyExtensionFieldValues,
  buildSidebarSections,
  defineExtensionFormField,
  extensionAgentLinks,
  extensionBranding,
  extensionFormFields,
  extensionNavItems,
  extensionNavOverrides,
  extensionProviderIcons,
  extensionShell,
  extensionTableColumns,
  initialExtensionFieldValues,
  isNavPathActive,
  installExtensionApi,
  installExtensionApis,
  readExtensionFieldValues,
  validateAppExtensions,
  validateExtensionConfig,
  validateExtensionFieldValues,
} from "./index";
import type { AppExtensionConfig } from "./index";
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

function extensionItem(key: string, order: number) {
  return { key, order, Component: () => null };
}

describe("installExtensionApi", () => {
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
    const undo = installExtensionApi(undefined);
    expect(() => undo()).not.toThrow();
  });

  // The gRPC replacement for a path override: the extension answers the operation
  // itself, and `invoke` — which is the only way the client reaches an operation —
  // uses its implementation instead of the RPC.
  it("answers an operation from the extension's own implementation", async () => {
    installExtensionApi({
      operations: { "namespaces.list": async () => [{ name: "extension", status: "Active" }] },
    });

    await expect(invoke("namespaces.list", {})).resolves.toEqual([
      { name: "extension", status: "Active" },
    ]);
  });

  it("undoes an operation override", async () => {
    const undo = installExtensionApi({
      operations: { "namespaces.list": async () => [{ name: "extension", status: "Active" }] },
    });
    undo();

    // Back to the default implementation, so the point is only that the answer is no
    // longer the extension's.
    //
    // Asserted as "not the extension's answer" rather than as a rejection, because the
    // default implementation reaches the network: this spec used to require the call
    // to *fail*, which held only while nothing was listening on the dev API address.
    // With a port-forward up it succeeded and returned the cluster's real namespaces,
    // and the suite failed for a reason that had nothing to do with overrides.
    await expect(
      invoke("namespaces.list", {}).catch(() => "the default implementation failed"),
    ).resolves.not.toEqual([{ name: "extension", status: "Active" }]);
  });

  it("points an HTTP endpoint at the extension's path", () => {
    installExtensionApi({ endpoints: { "chat.a2a": "/v2/a2a" } });
    expect(resolveEndpoint("chat.a2a", { namespace: "kagent", name: "k8s" })).toBe(
      "/v2/a2a",
    );
  });

  it("undoes an endpoint override", () => {
    const undo = installExtensionApi({ endpoints: { "chat.a2a": "/v2/a2a" } });
    undo();
    expect(resolveEndpoint("chat.a2a", { namespace: "kagent", name: "k8s" })).toBe(
      "/a2a/kagent/k8s",
    );
  });

  it("rewrites the base URL prefix of a request", async () => {
    installExtensionApi({ baseUrl: "https://example.test/v1/" });
    const result = await applyRequestTransforms(requestContext("agents.list"));
    expect(result.url).toBe(
      "https://example.test/v1/kagent.api.v1alpha1.AgentService/ListAgents",
    );
  });

  it("leaves a URL that is not under the app's base alone", async () => {
    installExtensionApi({ baseUrl: "https://example.test" });
    const result = await applyRequestTransforms(
      requestContext("agents.list", "https://elsewhere.test/agents"),
    );
    expect(result.url).toBe("https://elsewhere.test/agents");
  });

  it("applies a per-call request transform only to its own call", async () => {
    installExtensionApi({
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
    installExtensionApi({
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
  // bound for the extension's control plane from any other.
  it("runs the global hook after the base URL rewrite", async () => {
    let seen = "";
    installExtensionApi({
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
    installExtensionApi({
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

  it("splits the core run so an extension item lands at its order", () => {
    const sections = buildSidebarSections(core, [extensionItem("example", 250)]);
    expect(sections.map((section) => section.kind)).toEqual([
      "core",
      "extension",
      "core",
    ]);
  });

  it("puts an extension item ordered before everything first", () => {
    const sections = buildSidebarSections(core, [extensionItem("example", 50)]);
    expect(sections.map((section) => section.kind)).toEqual(["extension", "core"]);
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
  const tierField = defineExtensionFormField<string>({
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
    expect(initialExtensionFieldValues(fields)).toEqual({ tier: "standard" });
  });

  it("writes the value into the extension's own payload shape", () => {
    const payload = applyExtensionFieldValues(fields, { kind: "Agent" }, {
      tier: "regulated",
    });
    expect(payload).toEqual({
      kind: "Agent",
      metadata: { annotations: { "example/tier": "regulated" } },
    });
  });

  it("round-trips a value back out of a payload", () => {
    const payload = applyExtensionFieldValues(fields, {}, { tier: "restricted" });
    expect(readExtensionFieldValues(fields, payload)).toEqual({
      tier: "restricted",
    });
  });

  it("reports validation messages by field id", () => {
    expect(validateExtensionFieldValues(fields, { tier: "" })).toEqual({
      tier: "Pick a tier",
    });
    expect(validateExtensionFieldValues(fields, { tier: "standard" })).toEqual({});
  });
});

describe("composing several installed extensions", () => {
  const noopComponent = () => null;

  function extension(
    id: string,
    parts: Partial<AppExtensionConfig> = {},
  ): AppExtensionConfig {
    return { id, name: id, ...parts };
  }

  /*
   * The rule the whole array rests on, stated once per capability below: additive
   * contributions all take effect in order, singular ones are merged with the later
   * entry winning. Anything that got this backwards would mean installing a second
   * extension silently switched off part of the first.
   */

  it("concatenates nav items from every extension and re-sorts them", () => {
    const items = extensionNavItems([
      extension("a", { navItems: [extensionItem("late", 400)] }),
      extension("b", { navItems: [extensionItem("early", 150)] }),
    ]);

    expect(items.map((item) => item.key)).toEqual(["early", "late"]);
  });

  it("concatenates form fields for the form they target", () => {
    const field = (id: string) =>
      defineExtensionFormField<string>({
        id,
        formId: "app_agents_agentNew_agentForm",
        Component: noopComponent,
        defaultValue: "",
        fromPayload: () => "",
        toPayload: (payload) => payload,
      });

    const fields = extensionFormFields(
      [
        extension("a", { formFields: [field("one")] }),
        extension("b", { formFields: [field("two")] }),
      ],
      "app_agents_agentNew_agentForm",
    );

    expect(fields.map((f) => f.id)).toEqual(["one", "two"]);
  });

  it("concatenates table columns for the table they target", () => {
    const column = (id: string) => ({
      id,
      tableId: "app_agents_agentsList_table" as const,
      title: id,
      render: () => null,
    });

    const columns = extensionTableColumns(
      [
        extension("a", { tableColumns: [column("region")] }),
        extension("b", { tableColumns: [column("cluster")] }),
      ],
      "app_agents_agentsList_table",
    );

    expect(columns.map((c) => c.id)).toEqual(["region", "cluster"]);
  });

  it("merges shell regions, later extensions winning per region", () => {
    const firstHeader = noopComponent;
    const secondHeader = () => null;

    const shell = extensionShell([
      extension("a", { shell: { Header: firstHeader, Sidebar: noopComponent } }),
      extension("b", { shell: { Header: secondHeader } }),
    ]);

    expect(shell.Header).toBe(secondHeader);
    // The first extension still owns the region the second said nothing about.
    expect(shell.Sidebar).toBe(noopComponent);
  });

  it("merges branding field by field", () => {
    const branding = extensionBranding([
      extension("a", { branding: { appName: "First", AppIcon: noopComponent } }),
      extension("b", { branding: { appName: "Second" } }),
    ]);

    expect(branding.appName).toBe("Second");
    expect(branding.AppIcon).toBe(noopComponent);
  });

  it("merges agent links, so a partial redirect keeps the other links", () => {
    const chat = () => "/chat";
    const details = () => "/details";

    const links = extensionAgentLinks([
      extension("a", { agentLinks: { chat, details } }),
      extension("b", { agentLinks: { details: () => "/other" } }),
    ]);

    expect(links.chat).toBe(chat);
    expect(links.details).not.toBe(details);
  });

  it("merges provider icons, later extensions replacing a shared key", () => {
    const icons = extensionProviderIcons([
      extension("a", { providerIcons: { OpenAI: noopComponent, Anthropic: noopComponent } }),
      extension("b", { providerIcons: { OpenAI: () => null } }),
    ]);

    expect(Object.keys(icons).sort()).toEqual(["Anthropic", "OpenAI"]);
    expect(icons.OpenAI).not.toBe(noopComponent);
  });

  /*
   * Two levels deep, unlike the merges above, because an override is itself a table
   * of independent choices: one extension hiding an entry and another renaming it
   * should produce a hidden, renamed entry rather than whichever spoke last.
   */
  it("merges nav overrides per entry and per field", () => {
    const overrides = extensionNavOverrides([
      extension("a", { navOverrides: { agents: { hidden: true } } }),
      extension("b", { navOverrides: { agents: { label: "Workloads" } } }),
    ]);

    expect(overrides.agents).toEqual({ hidden: true, label: "Workloads" });
  });

  it("installs each extension's API config in order, so transforms compose", async () => {
    const undo = installExtensionApis([
      extension("a", {
        api: {
          request: (context) => ({
            ...context,
            headers: { ...context.headers, "x-first": "1" },
          }),
        },
      }),
      extension("b", {
        api: {
          request: (context) => ({
            ...context,
            headers: { ...context.headers, "x-second": "2" },
          }),
        },
      }),
    ]);

    const result = await applyRequestTransforms(
      { endpoint: "agents.list", method: "POST", url: "/x", headers: {} },
    );

    expect(result.headers).toEqual({ "x-first": "1", "x-second": "2" });
    undo();
    clearApiExtensions();
  });
});

describe("validateAppExtensions", () => {
  const page = { path: "/insights", element: createElement("div") };

  it("accepts an install of several extensions that do not collide", () => {
    expect(() =>
      validateAppExtensions([
        { id: "a", name: "A", navItems: [extensionItem("a", 10)] },
        { id: "b", name: "B", navItems: [extensionItem("b", 20)] },
      ]),
    ).not.toThrow();
  });

  it("rejects the same extension installed twice", () => {
    expect(() =>
      validateAppExtensions([
        { id: "a", name: "A" },
        { id: "a", name: "A" },
      ]),
    ).toThrow(/installed more than once/);
  });

  /*
   * Two individually valid configs that cannot be installed together. React Router
   * takes the first match and React warns about the duplicate key and carries on, so
   * without this the second extension's page is simply unreachable and nothing says
   * so — which is the whole class of failure the array introduces.
   */
  it("rejects two extensions claiming the same route path", () => {
    expect(() =>
      validateAppExtensions([
        { id: "a", name: "A", routes: [page] },
        { id: "b", name: "B", routes: [page] },
      ]),
    ).toThrow(/contributed by more than one extension/);
  });

  it("rejects two extensions contributing the same nav key", () => {
    expect(() =>
      validateAppExtensions([
        { id: "a", name: "A", navItems: [extensionItem("insights", 10)] },
        { id: "b", name: "B", navItems: [extensionItem("insights", 20)] },
      ]),
    ).toThrow(/contributed by more than one extension/);
  });

  it("names the extension a per-config problem came from", () => {
    try {
      validateAppExtensions([
        { id: "a", name: "A" },
        {
          id: "broken",
          name: "B",
          navItems: [extensionItem("dup", 10), extensionItem("dup", 20)],
        },
      ]);
      expect.unreachable("should have thrown");
    } catch (error) {
      expect((error as Error).message).toMatch(/broken: nav item key "dup"/);
    }
  });
});

describe("validateExtensionConfig", () => {
  const base: AppExtensionConfig = { id: "example", name: "Example" };
  const coreRoutePaths = reservedRoutePaths;

  it("accepts a config that contributes nothing", () => {
    expect(() => validateExtensionConfig(base)).not.toThrow();
  });

  it("rejects a slot naming an unknown extension point", () => {
    const config = {
      ...base,
      // Deliberately bypassing the typed key check, which is what a config
      // deserialised from JSON would do.
      slots: { app_agents_typo_notAPoint: () => null },
    } as unknown as AppExtensionConfig;

    expect(() => validateExtensionConfig(config)).toThrow(
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
      const config: AppExtensionConfig = {
        ...base,
        routes: [{ path, element: createElement("div") }],
      };
      expect(
        () => validateExtensionConfig(config, reservedRoutePaths),
        `a contributed route at "${path}" should be rejected without \`replaces\``,
      ).toThrow(/collides with a core route/);
    }
  });

  it("rejects a route colliding with a core one", () => {
    const config: AppExtensionConfig = {
      ...base,
      routes: [{ path: "/agents", element: createElement("div") }],
    };
    expect(() => validateExtensionConfig(config, ["/agents"])).toThrow(
      /collides with a core route/,
    );
  });

  it("rejects duplicate nav keys", () => {
    const config: AppExtensionConfig = {
      ...base,
      navItems: [extensionItem("dup", 10), extensionItem("dup", 20)],
    };
    expect(() => validateExtensionConfig(config)).toThrow(
      /declared twice/,
    );
  });

  it("reports every problem in one throw", () => {
    const config = {
      ...base,
      slots: { nope: () => null },
      navItems: [extensionItem("dup", 10), extensionItem("dup", 20)],
    } as unknown as AppExtensionConfig;

    try {
      validateExtensionConfig(config);
      expect.unreachable("should have thrown");
    } catch (error) {
      expect((error as Error).message).toMatch(/not a known extension point/);
      expect((error as Error).message).toMatch(/declared twice/);
    }
  });
});
