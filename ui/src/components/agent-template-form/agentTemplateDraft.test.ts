import { describe, expect, it } from "vitest";
import {
  builtinSkillDraft,
  draftFromTemplate,
  draftFromSpec,
  draftProblems,
  emptyDraft,
  emptySkillDraft,
  labelsFromDraft,
  matchBuiltinSkill,
  specFromDraft,
  type AgentTemplateDraft,
} from "./agentTemplateDraft";
import type { AgentTemplate } from "@/api/domain/agentTemplates";

/**
 * The agent-template draft, and the one property that is invisible on screen.
 *
 * An edit form still omits two `AgentTemplateSpec` fields. Building the update out of
 * only what it shows deletes those fields — `plugins`, `promptTemplate` — and the API
 * accepts that happily. Nothing on the page changes,
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

    expect(spec.plugins, "an edit must not delete plugins it never showed").toEqual(template.resource.spec.plugins);
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
    expect(spec.skills).toEqual(template.resource.spec.skills);
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
    expect(spec.plugins).toHaveLength(1);
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

  it.each([
    [undefined, false],
    [false, false],
    [true, true],
  ] as const)("reads outputSchemaStrict %s as %s and writes only true", (strict, expected) => {
    const template = templateWithExtras();
    const spec = { ...template.resource.spec, outputSchemaStrict: strict };
    const draft = draftFromTemplate({ ...template, resource: { ...template.resource, spec } });

    expect(draft.outputStrict).toBe(expected);
    const saved = specFromDraft(draft, spec);
    if (expected) expect(saved.outputSchemaStrict).toBe(true);
    else expect(saved).not.toHaveProperty("outputSchemaStrict");
  });

  it("keeps strict through an edit to another field and drops it with the schema", () => {
    const template = templateWithExtras();
    const spec = { ...template.resource.spec, outputSchemaStrict: true };
    const draft = draftFromTemplate({ ...template, resource: { ...template.resource, spec } });

    expect(specFromDraft({ ...draft, outputStrict: false }, spec)).not.toHaveProperty(
      "outputSchemaStrict",
    );
    const edited = specFromDraft({ ...draft, description: "Edited." }, spec);
    expect(edited.description).toBe("Edited.");
    expect(edited.outputSchemaStrict).toBe(true);

    const text = specFromDraft({ ...draft, outputSource: "text" }, spec);
    expect(text).not.toHaveProperty("outputSchema");
    expect(text).not.toHaveProperty("outputSchemaStrict");
  });

  it("keeps an MCP binding with no selection because it exposes every server tool", () => {
    const draft = emptyDraft("kagent");
    draft.modelConfig = { name: "gpt" };
    draft.mcpTools = [{ serverRef: { name: "tools", namespace: "kagent" }, tools: [] }];
    draft.subAgentTools = [
      { name: "", description: "", refName: "other" },
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
      name: "review", description: "Review changes", templateRef: {name: "review-context"},
    }}];
    const draft = draftFromTemplate(template);
    expect(draft.subAgentTools[0]).toMatchObject({refName: "review-context"});
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
    draft.modelConfig = { name: "gpt" };
    draft.mcpTools = [{ serverRef: { name: "tools", namespace: "kagent" }, tools: [], requireApproval: false }];

    expect(specFromDraft(draft).tools?.[0].mcp).toEqual({
      server: { kind: "RemoteMCPServer", name: "tools" },
    });
  });

  it("sends an MCP server by bare name, as the CRD's reference is same-namespace", () => {
    const draft = emptyDraft("kagent");
    draft.modelConfig = { name: "gpt" };
    draft.mcpTools = [{ serverRef: { name: "tools", namespace: "kagent" }, tools: ["a"] }];

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
    draft.modelConfig = { name: "gpt" };
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
    draft.modelConfig = { name: "gpt" };
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


describe("cross-namespace references", () => {
  it("preserves shared model and MCP identities across an edit", () => {
    const spec = {
      modelConfig: { name: "default", namespace: "shared" },
      tools: [{ mcp: { server: { kind: "RemoteMCPServer" as const, name: "tools", namespace: "shared" } } }],
    };
    const draft = draftFromSpec(spec, "team-a");
    expect(draft.modelConfig).toEqual({ name: "default", namespace: "shared" });
    expect(draft.mcpTools[0].serverRef).toEqual({ kind: "RemoteMCPServer", name: "tools", namespace: "shared" });
    const saved = specFromDraft(draft, spec);
    expect(saved.modelConfig).toEqual(spec.modelConfig);
    expect(saved.tools).toEqual(spec.tools);
  });

  // Dropping group or kind would point the reference at a same-named default object.
  it("keeps the group and kind of model and MCP references through an edit", () => {
    const spec = {
      modelConfig: { group: "example.com", kind: "Foo", name: "default" },
      tools: [{ mcp: { server: { group: "example.com", kind: "Foo", name: "tools" } } }],
    };
    const draft = draftFromSpec(spec, "team-a");
    draft.description = "edited";
    const saved = specFromDraft(draft, spec);
    expect(saved.modelConfig).toEqual(spec.modelConfig);
    expect(saved.tools).toEqual(spec.tools);
  });

  it("edits structured references without mutating the loaded resource", () => {
    const spec = {
      modelConfig: { name: "default", namespace: "shared" },
      tools: [{ mcp: { server: { kind: "RemoteMCPServer" as const, name: "tools", namespace: "shared" } } }],
    };
    const draft = draftFromSpec(spec, "team-a");
    draft.modelConfig!.namespace = "other";
    draft.mcpTools[0].serverRef.namespace = "other";
    const saved = specFromDraft(draft, spec);
    expect(saved.modelConfig).toEqual({ name: "default", namespace: "other" });
    expect(saved.tools?.[0].mcp?.server).toEqual({ kind: "RemoteMCPServer", name: "tools", namespace: "other" });
    expect(spec.modelConfig.namespace).toBe("shared");
    expect(spec.tools[0].mcp.server.namespace).toBe("shared");
  });

  it("omits local namespaces and clears an unselected model", () => {
    const draft = emptyDraft("team-a");
    draft.modelConfig = { name: "model", namespace: "team-a" };
    draft.mcpTools = [{ serverRef: { name: "tools", namespace: "team-a" }, tools: [] }];
    const saved = specFromDraft(draft);
    expect(saved.modelConfig).toEqual({ name: "model" });
    expect(saved.tools?.[0].mcp?.server).toEqual({ kind: "RemoteMCPServer", name: "tools" });
    draft.modelConfig = undefined;
    draft.mcpTools.push({ serverRef: { name: "" }, tools: [] });
    const cleared = specFromDraft(draft, saved);
    expect(cleared).not.toHaveProperty("modelConfig");
    expect(cleared.tools).toHaveLength(1);
  });
});

const DIGEST = "sha256:" + "0".repeat(64);
const CATALOG = {
  image: `ghcr.io/kagent-dev/kagent/tools@${DIGEST}`,
  skills: [{ name: "k8s-troubleshooting", description: "Diagnose unhealthy workloads.", path: "opt/kagent-tools-skills/skills/k8s-troubleshooting" }],
};

function draftWithSkills(...skills: Partial<AgentTemplateDraft["skills"][number]>[]): AgentTemplateDraft {
  const draft = emptyDraft("kagent");
  draft.name = "t";
  draft.skills = skills.map((skill) => ({ ...emptySkillDraft(), ...skill }));
  return draft;
}

describe("skills", () => {
  it("round-trips every source kind, with and without the optional fields", () => {
    const skills = [
      { name: "oci", source: { oci: `ghcr.io/org/skills@${DIGEST}` } },
      { name: "git", source: { git: { url: "https://github.com/org/skills.git", commit: "a".repeat(40) }, path: "skills/git" } },
      {
        name: "s3",
        source: {
          bucket: { s3: { endpoint: "https://s3.example.com", bucket: "b", key: "k.tgz", versionId: "v1", region: "eu-west-1" } },
        },
      },
      {
        name: "s3-no-region",
        source: { bucket: { s3: { endpoint: "https://s3.example.com", bucket: "b", key: "k.tgz", versionId: "v2" } }, path: "x" },
      },
    ];
    const draft = draftFromSpec({ skills }, "kagent");

    expect(draft.skills.map((skill) => skill.sourceKind)).toEqual(["oci", "git", "s3", "s3"]);
    expect(specFromDraft(draft, { skills }).skills).toEqual(skills);
  });

  it("trims what it sends, omits an empty path and region, and drops blank rows", () => {
    const draft = draftWithSkills(
      { name: " review ", sourceKind: "s3", s3Endpoint: " https://s3.example.com ", s3Bucket: "b", s3Key: "k", s3VersionId: "v", path: "  " },
      { sourceKind: "git" },
    );

    expect(specFromDraft(draft).skills).toEqual([
      { name: "review", source: { bucket: { s3: { endpoint: "https://s3.example.com", bucket: "b", key: "k", versionId: "v" } } } },
    ]);
    expect(draftProblems(draft, { isCreate: true })).toEqual([]);
  });

  it("removes spec.skills when the draft has none", () => {
    const spec = { skills: [{ name: "a", source: { oci: `ghcr.io/org/skills@${DIGEST}` } }] };
    const draft = draftFromSpec(spec, "kagent");
    draft.skills = [];

    expect(specFromDraft(draft, spec)).not.toHaveProperty("skills");
  });

  it("keeps the other kinds' text when the source kind changes", () => {
    const draft = draftWithSkills({ name: "a", sourceKind: "git", gitUrl: "https://x.test/r.git", gitCommit: "b".repeat(40) });
    draft.skills[0].sourceKind = "oci";
    draft.skills[0].sourceKind = "git";

    expect(specFromDraft(draft).skills?.[0].source).toEqual({ git: { url: "https://x.test/r.git", commit: "b".repeat(40) } });
  });

  it("writes a kagent skill as the catalog image and path, and recognizes it again", () => {
    const draft = draftWithSkills();
    draft.skills = [builtinSkillDraft(CATALOG, CATALOG.skills[0])];
    const spec = specFromDraft(draft);

    expect(spec.skills).toEqual([
      { name: "k8s-troubleshooting", source: { oci: CATALOG.image, path: "opt/kagent-tools-skills/skills/k8s-troubleshooting" } },
    ]);
    const reopened = draftFromSpec(spec, "kagent").skills[0];
    expect(matchBuiltinSkill(reopened, CATALOG)).toEqual({ skill: CATALOG.skills[0], current: true });
    expect(matchBuiltinSkill({ ...reopened, path: "opt/kagent-tools-skills/skills/other" }, CATALOG)).toBeUndefined();
    expect(matchBuiltinSkill({ ...reopened, name: "my-copy" }, CATALOG)).toBeUndefined();
    expect(matchBuiltinSkill({ ...reopened, oci: `ghcr.io/org/skills@${DIGEST}` }, CATALOG)).toBeUndefined();
    expect(matchBuiltinSkill(reopened, undefined)).toBeUndefined();
  });

  it("recognizes a kagent skill from an earlier image of the same repository as an older version", () => {
    const older = draftWithSkills({
      name: "k8s-troubleshooting",
      oci: `ghcr.io/kagent-dev/kagent/tools@sha256:${"1".repeat(64)}`,
      path: "opt/kagent-tools-skills/skills/k8s-troubleshooting",
    }).skills[0];

    expect(matchBuiltinSkill(older, CATALOG)).toEqual({ skill: CATALOG.skills[0], current: false });
  });

  it("reports what the CRD would refuse, naming the skill", () => {
    const draft = draftWithSkills(
      { name: "", oci: `ghcr.io/org/skills@${DIGEST}` },
      { name: "tagged", oci: "ghcr.io/org/skills:v1" },
      { name: "git", sourceKind: "git", gitUrl: "git@github.com:org/r.git", gitCommit: "abc123" },
      { name: "s3", sourceKind: "s3", s3Endpoint: "s3.example.com", s3Bucket: "b" },
      { name: "up", oci: `ghcr.io/org/skills@${DIGEST}`, path: "skills/../secrets" },
      { name: "abs", oci: `ghcr.io/org/skills@${DIGEST}`, path: "/skills" },
      { name: "up", oci: `ghcr.io/org/skills@${DIGEST}` },
      { name: "..", oci: `ghcr.io/org/skills@${DIGEST}` },
      { name: "a/b", oci: `ghcr.io/org/skills@${DIGEST}` },
      { name: "a\\b", oci: `ghcr.io/org/skills@${DIGEST}` },
      { name: "from-plugin", oci: `ghcr.io/org/skills@${DIGEST}` },
    );
    draft.pluginSkills = ["from-plugin"];

    expect(draftProblems(draft, { isCreate: true })).toEqual([
      "Skill 1 needs a name.",
      'The skill "tagged" needs an OCI image pinned by digest, like ghcr.io/org/skills@sha256:<64 hex characters>.',
      'The skill "git" needs a Git URL that starts with http:// or https://.',
      'The skill "git" needs a full Git commit ID: 40 or 64 hex characters.',
      'The skill "s3" needs an S3 endpoint that starts with http:// or https://.',
      'The skill "s3" needs an S3 key and version ID.',
      'The skill "up" needs a path relative to the artifact, without ".." segments.',
      'The skill "abs" needs a path relative to the artifact, without ".." segments.',
      'The skill ".." needs a name that is one path segment: not "." or "..", and without "/" or "\\".',
      'The skill "a/b" needs a name that is one path segment: not "." or "..", and without "/" or "\\".',
      'The skill "a\\b" needs a name that is one path segment: not "." or "..", and without "/" or "\\".',
      'More than one skill is named "up". Skill names must be unique.',
      'A plugin on this template already selects a skill named "from-plugin". Skill names must be unique.',
    ]);
  });

  it("checks names against the skills the existing spec's plugins select", () => {
    const spec = {
      skills: [{ name: "review", source: { oci: `ghcr.io/org/skills@${DIGEST}` } }],
      plugins: [{ source: { oci: `ghcr.io/org/plugin@${DIGEST}` }, skills: ["review"] }],
    };

    expect(draftProblems(draftFromSpec(spec, "kagent"), { isCreate: false })).toEqual([
      'A plugin on this template already selects a skill named "review". Skill names must be unique.',
    ]);
  });

  it("numbers a skill by its row, counting blank rows", () => {
    const draft = draftWithSkills({}, { oci: `ghcr.io/org/skills@${DIGEST}` });

    expect(draftProblems(draft, { isCreate: true })).toEqual(["Skill 2 needs a name."]);
  });

  it("treats a row as blank from its name, path and active source, and saves only that source", () => {
    const hidden = { sourceKind: "oci" as const, gitUrl: "https://x.test/r.git", s3Bucket: "b" };
    expect(specFromDraft(draftWithSkills(hidden))).not.toHaveProperty("skills");
    expect(draftProblems(draftWithSkills(hidden), { isCreate: true })).toEqual([]);

    const draft = draftWithSkills({ ...hidden, name: "a", oci: `ghcr.io/org/skills@${DIGEST}` });
    expect(specFromDraft(draft).skills).toEqual([{ name: "a", source: { oci: `ghcr.io/org/skills@${DIGEST}` } }]);
  });

  it("allows at most 50 skills", () => {
    const skills = Array.from({ length: 51 }, (_, index) => ({ name: `s${index}`, oci: `ghcr.io/org/skills@${DIGEST}` }));

    expect(draftProblems(draftWithSkills(...skills.slice(0, 50)), { isCreate: true })).toEqual([]);
    expect(draftProblems(draftWithSkills(...skills), { isCreate: true })).toEqual(["A template can have at most 50 skills."]);
  });
});
