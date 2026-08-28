import { describe, expect, it } from "vitest";
import type { AgentTemplate, AgentTemplateHarnessStatus } from "./agentTemplates";
import {
  agentPairsFrom,
  agentPairsOf,
  bareName,
  newConversationBlockedReason,
  pairIdOfInstance,
} from "./agentPairs";

/**
 * An agent is a pair, and this is where that claim is checked.
 *
 * The whole agents page is derived from these functions, so the properties worth
 * pinning are the ones a page cannot show you it got wrong: that one template can be
 * two agents, that a template nothing admits is no agent at all, and that a
 * conversation's pair is matched on bare names when the record reports qualified
 * refs. Each of those has a plausible wrong implementation that renders perfectly.
 */

function template(
  name: string,
  harnesses: AgentTemplateHarnessStatus[],
  namespace = "kagent",
): AgentTemplate {
  return {
    ref: `${namespace}/${name}`,
    namespace,
    name,
    modelConfigRef: `${namespace}/default-model-config`,
    description: `${name} does something`,
    admittingHarnesses: harnesses.map((entry) => entry.harness),
    resource: {
      metadata: { name, namespace },
      spec: { modelConfig: { name: "default-model-config" } },
      status: { observedGeneration: 1, harnesses },
    },
  };
}

describe("an agent is a template paired with a harness", () => {
  it("is one agent per admitting harness, so one template can be two agents", () => {
    const pairs = agentPairsOf(
      template("shared-brain", [
        { harness: "k8s-agent", desiredRevision: "r1", latestSuccessfulRevision: "r1" },
        { harness: "fast-lane", desiredRevision: "r2", latestSuccessfulRevision: "r2" },
      ]),
    );

    expect(pairs).toHaveLength(2);
    // Distinguished by the harness and by nothing else, which is exactly why a page
    // keyed on the template alone would merge them.
    expect(pairs.map((pair) => pair.harness).sort()).toEqual([
      "fast-lane",
      "k8s-agent",
    ]);
    expect(new Set(pairs.map((pair) => pair.agentTemplate))).toEqual(
      new Set(["shared-brain"]),
    );
    // Two distinct ids, or a table would render one row and drop the other.
    expect(new Set(pairs.map((pair) => pair.id)).size).toBe(2);
  });

  it("is no agent at all when nothing admits the template", () => {
    // Not an empty-looking agent, and not an agent marked broken: a template no
    // harness admits reaches no prepared revision and every CreateAgentInstance
    // naming it is refused, so there is nothing to run. It is still a template, and
    // the templates page is where that is said.
    expect(agentPairsOf(template("note-taker", []))).toEqual([]);
  });

  it("reads the revision state from the status entry, not from the harness name", () => {
    const [ready, preparing, silent] = agentPairsOf(
      template("mixed", [
        { harness: "a", desiredRevision: "r1", latestSuccessfulRevision: "r1" },
        {
          harness: "b",
          desiredRevision: "r2",
          conditions: [
            { type: "Ready", status: "False", reason: "Waiting", message: "still building" },
          ],
        },
        // `desiredRevision` is required by the CRD, but a record can still arrive
        // without it — and "the controller has not said" is a third answer, not a
        // failure. Reporting it as broken would be inventing a fact.
        { harness: "c" } as AgentTemplateHarnessStatus,
      ]),
    );

    expect(ready.revisionState).toBe("ready");
    expect(ready.latestSuccessfulRevision).toBe("r1");
    expect(preparing.revisionState).toBe("preparing");
    expect(preparing.notReadyReason).toBe("still building");
    expect(silent.revisionState).toBe("notReported");
  });

  it("orders agents so the two an ambiguous template is sit together", () => {
    const pairs = agentPairsFrom([
      template("zeta", [{ harness: "b", desiredRevision: "r" }]),
      template("alpha", [
        { harness: "z", desiredRevision: "r" },
        { harness: "a", desiredRevision: "r" },
      ]),
      template("gamma", [{ harness: "a", desiredRevision: "r" }], "analytics"),
    ]);

    // Namespace, then template, then harness — so the difference between two rows
    // from one template is the column that changes, and a refetch cannot rearrange
    // the list under a reader's cursor.
    expect(pairs.map((pair) => pair.id)).toEqual([
      "analytics/gamma/a",
      "kagent/alpha/a",
      "kagent/alpha/z",
      "kagent/zeta/b",
    ]);
  });
});

describe("the pair a conversation belongs to", () => {
  it("matches on bare names, because an instance reports qualified refs", () => {
    // The failure this guards is silent: comparing the reported `namespace/name`
    // against a pair id built from bare names matches nothing, so every conversation
    // would count as belonging to no agent and every count would read zero.
    expect(
      pairIdOfInstance({
        namespace: "kagent",
        agentTemplate: "kagent/k8s-agent-7f3a91c",
        harness: "kagent/k8s-agent",
      }),
    ).toBe("kagent/k8s-agent-7f3a91c/k8s-agent");
  });

  it("is undefined when the record names no pair", () => {
    // An instance with no prepared revision belongs to no pair — the controller's
    // own list query left-joins the revision — so this is a real answer rather than
    // a value that failed to arrive.
    expect(pairIdOfInstance({ namespace: "kagent" })).toBeUndefined();
    expect(
      pairIdOfInstance({ namespace: "kagent", agentTemplate: "kagent/only-half" }),
    ).toBeUndefined();
  });

  it("leaves an already-bare name alone", () => {
    expect(bareName("k8s-agent")).toBe("k8s-agent");
    expect(bareName("kagent/k8s-agent")).toBe("k8s-agent");
  });
});

describe("whether a conversation can be started", () => {
  it("says nothing blocks a ready agent", () => {
    const [pair] = agentPairsOf(
      template("t", [{ harness: "h", desiredRevision: "r", latestSuccessfulRevision: "r" }]),
    );
    expect(newConversationBlockedReason(pair)).toBeUndefined();
  });

  it("gives the controller's own reason while a revision is preparing", () => {
    // CreateAgentInstance answers FailedPrecondition for a pair with no successful
    // revision, so the button is disabled with a reason rather than enabled into a
    // refusal — and the reason is the controller's words where it gave any.
    const [pair] = agentPairsOf(
      template("t", [
        {
          harness: "h",
          desiredRevision: "r",
          conditions: [
            {
              type: "Ready",
              status: "False",
              reason: "ActorTemplateNotReady",
              message: "Waiting for the golden snapshot.",
            },
          ],
        },
      ]),
    );
    expect(newConversationBlockedReason(pair)).toBe("Waiting for the golden snapshot.");
  });

  it("does not claim a failure when the controller has simply said nothing", () => {
    const [pair] = agentPairsOf(
      template("t", [{ harness: "h" } as AgentTemplateHarnessStatus]),
    );
    const reason = newConversationBlockedReason(pair);
    expect(reason).toMatch(/has not reported a revision/);
    expect(reason).not.toMatch(/fail|broken|error/i);
  });
});
