import { describe, expect, it } from "vitest";
import type { AgentResponse, ToolsResponse } from "@/api";
import {
  advertisedSkills,
  describeOciRef,
  skillSources,
  toolBindings,
  toolCount,
} from "./agentCapabilities";

/**
 * The tool rows are shaped as a real controller returns them, not as the browser
 * fixtures happened to: `id` is the bare tool name and `server_name` is a
 * `namespace/name` ref. Captured from a live cluster — a suite written against the
 * fixtures' `ref/name` ids passes while nothing matches in production.
 */
function row(server: string, id: string, description: string): ToolsResponse {
  return {
    id,
    server_name: server,
    description,
    group_kind: "RemoteMCPServer.kagent.dev",
    created_at: "",
    updated_at: "",
    deleted_at: "",
  };
}

function agent(spec: Partial<AgentResponse> & { namespace?: string } = {}): AgentResponse {
  const { namespace = "kagent", ...rest } = spec;
  return {
    id: 1,
    model: "gpt-4.1",
    modelProvider: "OpenAI",
    modelConfigRef: "kagent/default",
    deploymentReady: true,
    accepted: true,
    tools: [],
    agent: {
      apiVersion: "kagent.dev/v1alpha2",
      kind: "Agent",
      metadata: { name: "k8s-agent", namespace },
      spec: {
        type: "Declarative",
        description: "",
        declarative: { modelConfig: "default", systemMessage: "", tools: [] },
      },
    },
    ...rest,
  } as AgentResponse;
}

describe("toolBindings", () => {
  it("puts a description against each named tool", () => {
    const bindings = toolBindings(
      agent({
        tools: [
          {
            type: "McpServer",
            mcpServer: {
              name: "kagent-tool-server",
              namespace: "kagent",
              toolNames: ["helm_uninstall", "k8s_get_pods"],
              requireApproval: ["helm_uninstall"],
            },
          },
        ],
      }),
      [
        row("kagent/kagent-tool-server", "helm_uninstall", "Uninstall a Helm release"),
        row("kagent/kagent-tool-server", "k8s_get_pods", "List pods."),
        row("platform/grafana-mcp", "k8s_get_pods", "A different server's tool."),
      ],
    );

    expect(bindings).toHaveLength(1);
    expect(bindings[0].ref).toBe("kagent/kagent-tool-server");
    expect(bindings[0].tools).toEqual([
      {
        name: "helm_uninstall",
        description: "Uninstall a Helm release",
        requiresApproval: true,
      },
      { name: "k8s_get_pods", description: "List pods.", requiresApproval: false },
    ]);
  });

  it("takes the namespace from the agent when the binding leaves it out", () => {
    const bindings = toolBindings(
      agent({
        namespace: "platform",
        tools: [
          {
            type: "McpServer",
            mcpServer: { name: "grafana-mcp", toolNames: ["query_prometheus"] },
          },
        ],
      }),
      [row("platform/grafana-mcp", "query_prometheus", "Run a PromQL query.")],
    );

    expect(bindings[0].ref).toBe("platform/grafana-mcp");
    expect(bindings[0].tools[0].description).toBe("Run a PromQL query.");
  });

  it("does not borrow a description from a same-named tool on another server", () => {
    const bindings = toolBindings(
      agent({
        tools: [
          {
            type: "McpServer",
            mcpServer: { name: "grafana-mcp", namespace: "platform", toolNames: ["query"] },
          },
        ],
      }),
      [row("analytics/warehouse-mcp", "query", "The wrong query tool.")],
    );

    expect(bindings[0].tools[0].description).toBeUndefined();
  });

  it("still names the tools while the catalogue is loading", () => {
    const bindings = toolBindings(
      agent({
        tools: [
          {
            type: "McpServer",
            mcpServer: { name: "s", namespace: "kagent", toolNames: ["a", "b"] },
          },
        ],
      }),
      undefined,
    );

    expect(bindings[0].tools.map((tool) => tool.name)).toEqual(["a", "b"]);
    expect(bindings[0].tools[0].description).toBeUndefined();
  });

  it("keeps a named tool the catalogue has no row for", () => {
    // A server that is not answering has no rows, but the agent is still configured to
    // call these — reporting no tools would describe the agent wrongly.
    const bindings = toolBindings(
      agent({
        tools: [
          {
            type: "McpServer",
            mcpServer: { name: "down", namespace: "kagent", toolNames: ["still_bound"] },
          },
        ],
      }),
      [],
    );

    expect(bindings[0].tools).toEqual([
      { name: "still_bound", description: undefined, requiresApproval: false },
    ]);
  });

  it("reads the server's whole set when the binding names none", () => {
    const bindings = toolBindings(
      agent({
        tools: [
          { type: "McpServer", mcpServer: { name: "grafana-mcp", namespace: "platform" } },
        ],
      }),
      [
        row("platform/grafana-mcp", "query_prometheus", "Query."),
        row("platform/grafana-mcp", "list_dashboards", "List."),
        row("kagent/other", "ignored", "Another server."),
      ],
    );

    expect(bindings[0].takesEverything).toBe(true);
    expect(bindings[0].tools.map((tool) => tool.name)).toEqual([
      "query_prometheus",
      "list_dashboards",
    ]);
  });

  it("lists a delegated agent as itself", () => {
    const bindings = toolBindings(
      agent({ tools: [{ type: "Agent", agent: { name: "promql-agent" } }] }),
      [],
    );

    expect(bindings[0]).toEqual({
      ref: "kagent/promql-agent",
      kind: "agent",
      tools: [],
      takesEverything: false,
    });
    // One capability, not zero: the agent is the tool.
    expect(toolCount(bindings)).toBe(1);
  });
});

describe("skills", () => {
  it("separates advertised skills from skill packages", () => {
    const subject = agent();
    subject.agent.spec.declarative!.a2aConfig = {
      skills: [
        {
          id: "promql",
          name: "PromQL",
          description: "Writes queries.",
          tags: ["metrics"],
          examples: ["p99 latency for my-service"],
          inputModes: [],
          outputModes: [],
        },
      ],
    };
    subject.agent.spec.declarative!.skills = {
      refs: ["ghcr.io/kagent-dev/skills/istio:0.4.1"],
      gitRefs: [{ url: "https://github.com/example/skills", ref: "main", path: "sre" }],
    };

    expect(advertisedSkills(subject).map((skill) => skill.name)).toEqual(["PromQL"]);
    expect(skillSources(subject)).toEqual({
      oci: ["ghcr.io/kagent-dev/skills/istio:0.4.1"],
      git: [{ url: "https://github.com/example/skills", ref: "main", path: "sre" }],
    });
  });

  it("reports nothing rather than throwing when an agent has neither", () => {
    expect(advertisedSkills(agent())).toEqual([]);
    expect(skillSources(agent())).toEqual({ oci: [], git: [] });
  });
});

describe("describeOciRef", () => {
  it.each([
    ["ghcr.io/kagent-dev/skills/istio:0.4.1", "kagent-dev/skills/istio", "0.4.1"],
    ["kagent-dev/skills/istio", "kagent-dev/skills/istio", "latest"],
    ["localhost:5000/skills/dev:test", "skills/dev", "test"],
  ])("splits %s", (ref, name, version) => {
    expect(describeOciRef(ref)).toEqual({ name, version });
  });

  it("shortens a digest rather than showing all of it", () => {
    const { version } = describeOciRef("ghcr.io/x/y@sha256:0123456789abcdef0123456789abcdef");
    expect(version).toBe("sha256:0123456789ab…");
  });

  it("returns anything unparseable whole", () => {
    expect(describeOciRef("::::")).toEqual({ name: "::::" });
  });
});
