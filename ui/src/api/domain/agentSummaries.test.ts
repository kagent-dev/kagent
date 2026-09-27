import { describe, expect, it } from "vitest";
import type { AgentTemplate } from "./agentTemplates";
import type { Agent, AgentStatus } from "./agents";
import { agentSummariesFrom, agentSummaryOf, newConversationBlockedReason, agentRefOfInstance } from "./agentSummaries";

function agent(name: string, status?: AgentStatus): Agent {
  return {name, namespace: "team", ref: `team/${name}`, resource: {metadata: {name, namespace:"team"}, spec: {templateRef:{name:"shared"}, harnessRef:{name:"runner"}}, status}};
}

describe("explicit Agent catalog", () => {
  it("keeps distinct Agents using identical shared configuration", () => {
    expect(agentSummariesFrom([agent("second"), agent("first")]).map(row => row.id)).toEqual(["team/first", "team/second"]);
    expect(agentRefOfInstance({agent:"team/first"})).toBe("team/first");
    expect(agentRefOfInstance({})).toBeUndefined();
  });
  it("allows new conversations from the last successful revision after a failed edit", () => {
    const row = agentSummaryOf(agent("a", {latestSuccessfulRevision:"good", desiredRevision:"new", conditions:[{type:"Ready",status:"False",message:"Compilation failed"}]}));
    expect(row.notReadyReason).toBe("Compilation failed");
    expect(newConversationBlockedReason(row)).toBeUndefined();
  });
  it("reports a preparation failure before any successful revision", () => {
    const row = agentSummaryOf(agent("a", {desiredRevision:"new",conditions:[{type:"Compatible",status:"False",message:"Unsupported output schema"}]}));
    expect(newConversationBlockedReason(row)).toBe("Unsupported output schema");
  });
  it("supports inline configuration without inventing resource references", () => {
    const definition = agent("a");
    definition.resource.spec = {template:{description:"Inline behavior"}, harnessRef:{name:"runner"}};
    const row = agentSummaryOf(definition);
    expect(row.agentTemplate).toBe("Inline");
    expect(row.description).toBe("Inline behavior");
  });
  it("resolves descriptions only from the referenced template in the Agent namespace", () => {
    const template = (namespace: string, description: string): AgentTemplate => ({
      ref: `${namespace}/shared`, name: "shared", namespace, description, modelConfigRef: "",
      resource: {metadata: {name: "shared", namespace}, spec: {description}},
    });
    const templates = [template("other", "Wrong namespace"), template("team", "Reusable behavior")];
    expect(agentSummaryOf(agent("a"), templates).description).toBe("Reusable behavior");
    expect(agentSummaryOf(agent("a"), templates.slice(0, 1)).description).toBe("");
  });

});
