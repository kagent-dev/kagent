import { describe, expect, it } from "vitest";
import type { Agent, AgentStatus } from "./agents";
import { agentPairsFrom, agentPairsOf, newConversationBlockedReason, pairIdOfInstance } from "./agentPairs";

function agent(name: string, status?: AgentStatus): Agent {
  return {name, namespace: "team", ref: `team/${name}`, resource: {metadata: {name, namespace:"team"}, spec: {templateRef:{name:"shared"}, harnessRef:{name:"runner"}}, status}};
}

describe("explicit Agent catalog", () => {
  it("keeps distinct Agents using identical shared configuration", () => {
    expect(agentPairsFrom([agent("second"), agent("first")]).map(row => row.id)).toEqual(["team/first", "team/second"]);
    expect(pairIdOfInstance({agent:"team/first"})).toBe("team/first");
    expect(pairIdOfInstance({})).toBeUndefined();
  });
  it("allows new conversations from the last successful revision after a failed edit", () => {
    const [row] = agentPairsOf(agent("a", {latestSuccessfulRevision:"good", desiredRevision:"new", conditions:[{type:"Ready",status:"False",message:"Compilation failed"}]}));
    expect(row.notReadyReason).toBe("Compilation failed");
    expect(newConversationBlockedReason(row)).toBeUndefined();
  });
  it("reports a preparation failure before any successful revision", () => {
    const [row] = agentPairsOf(agent("a", {desiredRevision:"new",conditions:[{type:"Compatible",status:"False",message:"Unsupported output schema"}]}));
    expect(newConversationBlockedReason(row)).toBe("Unsupported output schema");
  });
  it("supports inline configuration without inventing resource references", () => {
    const definition = agent("a");
    definition.resource.spec = {template:{description:"Inline behavior"}, harnessRef:{name:"runner"}};
    const [row] = agentPairsOf(definition);
    expect(row.agentTemplate).toBe("Inline");
    expect(row.description).toBe("Inline behavior");
  });
});
