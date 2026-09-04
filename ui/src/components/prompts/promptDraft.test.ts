import { describe, expect, it } from "vitest";
import {
  type PromptDraft,
  emptyPromptDraft,
  promptDraftFrom,
  promptDraftIssues,
  promptPayloadFrom,
} from "./promptDraft";
import { newFragmentRow } from "./fragmentRows";

/** A draft that would be accepted, plus any overrides. */
const draft = (overrides: Partial<PromptDraft> = {}): PromptDraft => ({
  namespace: "kagent",
  name: "team-prompts",
  rows: [{ ...newFragmentRow(), key: "tone", value: "Be concise." }],
  ...overrides,
});

describe("promptDraftFrom", () => {
  it("carries the library's identity and its fragments", () => {
    const seeded = promptDraftFrom({
      namespace: "platform",
      name: "incident-playbooks",
      data: { triage: "Blast radius first.", postmortem: "Timeline." },
    });
    expect(seeded.namespace).toBe("platform");
    expect(seeded.name).toBe("incident-playbooks");
    expect(seeded.rows.map((row) => row.key)).toEqual(["postmortem", "triage"]);
  });
});

describe("promptDraftIssues", () => {
  it("accepts a complete draft", () => {
    expect(promptDraftIssues(draft())).toEqual([]);
  });

  it("requires a namespace and a name that Kubernetes would accept", () => {
    expect(promptDraftIssues(draft({ namespace: "" }))).toContain(
      "A namespace is required.",
    );
    expect(promptDraftIssues(draft({ name: "" }))).toContain(
      "A library name is required.",
    );
    expect(promptDraftIssues(draft({ name: "Team Prompts" }))).toEqual([
      "The name must use lowercase letters, numbers and hyphens, starting and ending with a letter or number.",
    ]);
  });

  it("skips the identity rules when the identity is locked", () => {
    // An edit addresses a library that already exists: its name and namespace are
    // the resource's own, shown but not editable, so they cannot be wrong. Marking
    // them would be complaining about a field the reader cannot change.
    const locked = draft({ namespace: "", name: "" });
    expect(promptDraftIssues(locked, { identityLocked: true })).toEqual([]);
  });

  it("applies the fragment rules either way, because the controller does", () => {
    const empty = draft({ rows: [newFragmentRow()] });
    for (const options of [{}, { identityLocked: true }]) {
      expect(promptDraftIssues(empty, options)).toContain(
        "Add at least one fragment key.",
      );
    }
  });
});

describe("promptPayloadFrom", () => {
  it("trims the identity and folds the rows into the data map", () => {
    const payload = promptPayloadFrom(
      draft({
        namespace: " kagent ",
        name: " team-prompts ",
        rows: [
          { ...newFragmentRow(), key: " tone ", value: "Be concise." },
          // A blank trailing row is how the editor normally looks, not something to
          // send an empty key for.
          newFragmentRow(),
        ],
      }),
    );
    expect(payload).toEqual({
      namespace: "kagent",
      name: "team-prompts",
      data: { tone: "Be concise." },
    });
  });
});

describe("emptyPromptDraft", () => {
  it("starts in the default namespace with one row to type into", () => {
    const fresh = emptyPromptDraft();
    expect(fresh.namespace).toBe("kagent");
    expect(fresh.name).toBe("");
    expect(fresh.rows).toHaveLength(1);
  });
});
