import { describe, expect, it } from "vitest";
import type { AgentTemplate } from "./agentTemplates";
import { agentDescription, agentRevisionState, newConversationBlockedReason, type Agent, type AgentStatus } from "./agents";

function agent(status?: AgentStatus): Agent {
  return { name: "a", namespace: "team", ref: "team/a", resource: { metadata: { name: "a", namespace: "team" }, spec: { templateRef: { name: "shared" }, harnessRef: { name: "runner" } }, status } };
}

describe("Agent readiness", () => {
  it("allows new conversations from the last successful revision after a failed edit", () => {
    const row = agent({ latestSuccessfulRevision: "good", desiredRevision: "new", conditions: [{ type: "Compatible", status: "False", message: "Compilation failed" }] });
    expect(agentRevisionState(row)).toBe("updateFailed");
    expect(newConversationBlockedReason(row)).toBeUndefined();
  });
  it("reports a preparation failure before any successful revision", () => {
    const row = agent({ desiredRevision: "new", conditions: [{ type: "Compatible", status: "False", message: "Unsupported output schema" }] });
    expect(agentRevisionState(row)).toBe("failed");
    expect(newConversationBlockedReason(row)).toBe("Unsupported output schema");
  });
  it("treats a pending golden snapshot as preparing, not failed", () => {
    const row = agent({ desiredRevision: "new", conditions: [{ type: "Ready", status: "False", reason: "ActorTemplatePending" }] });
    expect(agentRevisionState(row)).toBe("preparing");
  });
  it("reports a new revision still being prepared as updating", () => {
    expect(agentRevisionState(agent({ latestSuccessfulRevision: "good", desiredRevision: "new", conditions: [{ type: "Ready", status: "False", reason: "ActorTemplatePending" }] }))).toBe("updating");
  });
  it("reports an edit the controller has not observed yet as updating", () => {
    const row = agent({ observedGeneration: 1, latestSuccessfulRevision: "good", desiredRevision: "good" });
    row.resource.metadata.generation = 2;
    expect(agentRevisionState(row)).toBe("updating");
  });
  it("treats any Ready failure other than a pending snapshot as failed", () => {
    expect(agentRevisionState(agent({ desiredRevision: "new", conditions: [{ type: "Ready", status: "False", reason: "ActorTemplateConflict" }] }))).toBe("failed");
  });
  it("blocks an Agent the controller has not reported on", () => {
    expect(agentRevisionState(agent())).toBe("notReported");
    expect(newConversationBlockedReason(agent())).toMatch(/not reported/);
  });
  it("resolves descriptions only from the referenced template in the Agent namespace", () => {
    const template = (namespace: string, description: string): AgentTemplate => ({
      ref: `${namespace}/shared`, name: "shared", namespace, description, modelConfigRef: "",
      resource: { metadata: { name: "shared", namespace }, spec: { description } },
    });
    const templates = [template("other", "Wrong namespace"), template("team", "Reusable behavior")];
    expect(agentDescription(agent(), templates)).toBe("Reusable behavior");
    expect(agentDescription(agent(), templates.slice(0, 1))).toBeUndefined();
  });
});
