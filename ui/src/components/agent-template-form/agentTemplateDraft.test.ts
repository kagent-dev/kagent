import { describe, expect, it } from "vitest";
import {
  draftFromTemplate,
  draftProblems,
  emptyDraft,
  labelsFromDraft,
  specFromDraft,
} from "./agentTemplateDraft";
import type { AgentTemplate } from "@/api/domain/agentTemplates";

/**
 * The agent-template draft, and the one property that is invisible on screen.
 *
 * An edit form still omits three rich `AgentTemplateSpec` fields. Building the update
 * out of only what it shows deletes those fields — `skills`, `plugins`,
 * `promptTemplate` — and the API accepts that happily. Nothing on the page changes,
 * no error is raised, and the loss only appears on the cluster.
 *
 * This repository has made that mistake once already, on the agent form. So the
 * round trip is pinned here rather than left to be noticed.
 */

/** A template carrying rather more than the form models. */
function templateWithExtras(): AgentTemplate {
  return {
    ref: "kagent/rich",
    namespace: "kagent",
    name: "rich",
    modelConfigRef: "kagent/gpt",
    description: "A template with fields no form shows.",
    resource: {
      metadata: {
        name: "rich",
        namespace: "kagent",
        labels: { "kagent.dev/runtime": "runner" },
      },
      spec: {
        modelConfig: { name: "gpt" },
        description: "A template with fields no form shows.",
        systemPrompt: "Be brief.",
        outputSchema: {
          type: "object",
          properties: { status: { type: "string" } },
          required: ["status"],
        },
        tools: [
          {
            mcp: {
              server: { kind: "RemoteMCPServer", name: "tools" },
              tools: ["k8s_get_pods"],
              requireApproval: true,
            },
          },
        ],
        skills: [
          {
            name: "incident-review",
            source: {
              oci: "ghcr.io/example/skills@sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
            },
          },
        ],
        plugins: [{ source: { git: { url: "https://example.com/p", commit: "a".repeat(40) } } }],
        promptTemplate: { dataSources: [{ name: "runbooks" }] },
      },
    },
  };
}

describe("the agent template draft", () => {
  it("keeps the fields the form does not show", () => {
    const template = templateWithExtras();
    const draft = draftFromTemplate(template);

    // A save with nothing edited at all.
    const spec = specFromDraft(draft, template.resource.spec);

    // The three the form never displays, still there.
    expect(spec.skills, "an edit must not delete skills it never showed").toEqual(
      template.resource.spec.skills,
    );
    expect(spec.plugins).toEqual(template.resource.spec.plugins);
    expect(spec.promptTemplate).toEqual(template.resource.spec.promptTemplate);
  });

  it("round-trips the fields it does show", () => {
    const template = templateWithExtras();
    const spec = specFromDraft(
      draftFromTemplate(template),
      template.resource.spec,
    );

    expect(spec.modelConfig).toEqual({ name: "gpt" });
    expect(spec.systemPrompt).toBe("Be brief.");
    expect(spec.outputSchema).toEqual(template.resource.spec.outputSchema);
    expect(spec.tools).toEqual(template.resource.spec.tools);
  });

  it("drops what it does show when the draft empties it", () => {
    const template = templateWithExtras();
    const draft = draftFromTemplate(template);
    draft.systemPrompt = "";
    draft.mcpTools = [];

    const spec = specFromDraft(draft, template.resource.spec);

    // Removed, not written as an empty string: an empty value is a value the CRD
    // stores and shows back, where an absent field is the default.
    expect(spec).not.toHaveProperty("systemPrompt");
    expect(spec).not.toHaveProperty("tools");
    // And still not at the cost of the fields it does not model.
    expect(spec.skills).toHaveLength(1);
  });

  it("never sends both prompt sources, because the CRD rejects both", () => {
    const template = templateWithExtras();
    const draft = draftFromTemplate(template);

    // Inline → ConfigMap.
    draft.promptSource = "configMap";
    draft.systemPromptConfigMap = "prompts";
    draft.systemPromptKey = "triage";
    const toConfigMap = specFromDraft(draft, template.resource.spec);
    expect(toConfigMap.systemPromptFrom).toEqual({ name: "prompts", key: "triage" });
    expect(
      toConfigMap,
      "the inline prompt must be removed, not left beside the ConfigMap one",
    ).not.toHaveProperty("systemPrompt");

    // ConfigMap → inline, starting from the spec that has one.
    const back = draftFromTemplate({
      ...template,
      resource: { ...template.resource, spec: toConfigMap },
    });
    expect(back.promptSource).toBe("configMap");
    back.promptSource = "inline";
    back.systemPrompt = "Be brief.";
    const toInline = specFromDraft(back, toConfigMap);
    expect(toInline.systemPrompt).toBe("Be brief.");
    expect(toInline).not.toHaveProperty("systemPromptFrom");
  });

  it("round-trips each output mode without ever sending both schema sources", () => {
    const template = templateWithExtras();
    const inlineDraft = draftFromTemplate(template);

    expect(inlineDraft.outputSource).toBe("inline");
    expect(JSON.parse(inlineDraft.outputSchema)).toEqual(
      template.resource.spec.outputSchema,
    );
    const inline = specFromDraft(inlineDraft, template.resource.spec);
    expect(inline.outputSchema).toEqual(template.resource.spec.outputSchema);
    expect(inline).not.toHaveProperty("outputSchemaFrom");

    inlineDraft.outputSource = "configMap";
    inlineDraft.outputSchemaConfigMap = "schemas";
    inlineDraft.outputSchemaKey = "result.json";
    const fromConfigMap = specFromDraft(inlineDraft, inline);
    expect(fromConfigMap.outputSchemaFrom).toEqual({
      name: "schemas",
      key: "result.json",
    });
    expect(fromConfigMap).not.toHaveProperty("outputSchema");

    const configMapDraft = draftFromTemplate({
      ...template,
      resource: { ...template.resource, spec: fromConfigMap },
    });
    expect(configMapDraft.outputSource).toBe("configMap");
    configMapDraft.outputSource = "text";
    const text = specFromDraft(configMapDraft, fromConfigMap);
    expect(text).not.toHaveProperty("outputSchema");
    expect(text).not.toHaveProperty("outputSchemaFrom");
  });

  it("keeps an MCP binding with no selection because it exposes every server tool", () => {
    const draft = emptyDraft("kagent");
    draft.modelConfig = "gpt";
    draft.mcpTools = [{ serverRef: "kagent/tools", tools: [] }];
    draft.subAgentTools = [
      { name: "", description: "", templateName: "other", isolation: "Shared" },
    ];

    expect(specFromDraft(draft).tools).toEqual([
      {
        mcp: {
          server: { kind: "RemoteMCPServer", name: "tools" },
        },
      },
    ]);
  });

  it("round-trips subagent template bindings through the editor", () => {
    const template = templateWithExtras();
    template.resource.spec.tools = [{subAgent: {
      name: "review", description: "Review changes", templateRef: {name: "review-context"}, isolation: "Shared",
    }}];
    const draft = draftFromTemplate(template);
    expect(draft.subAgentTools[0].templateName).toBe("review-context");
    expect(specFromDraft(draft, template.resource.spec).tools).toEqual(template.resource.spec.tools);
  });

  it("round-trips an MCP binding that exposes every server tool", () => {
    const template = templateWithExtras();
    template.resource.spec.tools = [
      { mcp: { server: { kind: "RemoteMCPServer", name: "tools" } } },
    ];

    const spec = specFromDraft(draftFromTemplate(template), template.resource.spec);

    expect(spec.tools).toEqual(template.resource.spec.tools);
  });

  it("omits requireApproval when it is off, because the CRD's default is false", () => {
    const draft = emptyDraft("kagent");
    draft.modelConfig = "gpt";
    draft.mcpTools = [{ serverRef: "kagent/tools", tools: [], requireApproval: false }];

    expect(specFromDraft(draft).tools?.[0].mcp).toEqual({
      server: { kind: "RemoteMCPServer", name: "tools" },
    });
  });

  it("sends an MCP server by bare name, as the CRD's reference is same-namespace", () => {
    const draft = emptyDraft("kagent");
    draft.modelConfig = "gpt";
    draft.mcpTools = [{ serverRef: "kagent/tools", tools: ["a"] }];

    expect(specFromDraft(draft).tools?.[0].mcp?.server).toEqual({
      kind: "RemoteMCPServer",
      name: "tools",
    });
  });

  it("reports what the controller would refuse, and nothing it would accept", () => {
    const draft = emptyDraft("kagent");
    expect(draftProblems(draft, { isCreate: true })).toEqual([
      "A name is required.",
    ]);

    draft.name = "t";
    draft.modelConfig = "gpt";
    // No labels, no tools, no description: all of that is a valid template.
    expect(draftProblems(draft, { isCreate: true })).toEqual([]);

    // Half a ConfigMap reference is not a reference.
    draft.promptSource = "configMap";
    draft.systemPromptConfigMap = "prompts";
    expect(draftProblems(draft, { isCreate: true })).toHaveLength(1);
  });

  it("reports incomplete or malformed structured-output configuration", () => {
    const draft = emptyDraft("kagent");
    draft.name = "t";
    draft.modelConfig = "gpt";
    draft.outputSource = "inline";

    expect(draftProblems(draft, { isCreate: true })).toContain(
      "An inline output schema is required.",
    );

    draft.outputSchema = "not json";
    expect(draftProblems(draft, { isCreate: true })).toContain(
      "The inline output schema must be a valid JSON object.",
    );

    draft.outputSchema = '{"type":"string"}';
    expect(draftProblems(draft, { isCreate: true })).toContain(
      'The output schema must have "type": "object" at its root.',
    );

    draft.outputSchema = '{"type":"object"}';
    expect(draftProblems(draft, { isCreate: true })).toEqual([]);

    draft.outputSource = "configMap";
    draft.outputSchemaConfigMap = "schemas";
    expect(draftProblems(draft, { isCreate: true })).toContain(
      "An output schema read from a ConfigMap needs both the ConfigMap's name and the key inside it.",
    );
  });

  it("carries labels through, because they decide whether anything runs it", () => {
    const draft = emptyDraft("kagent");
    draft.labels = [
      { key: "kagent.dev/runtime", value: "runner" },
      // Blank keys are dropped rather than sent as "".
      { key: "  ", value: "ignored" },
    ];

    expect(labelsFromDraft(draft)).toEqual({ "kagent.dev/runtime": "runner" });
  });
});
