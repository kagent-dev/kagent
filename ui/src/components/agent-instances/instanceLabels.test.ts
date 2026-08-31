/**
 * The wording an agent instance is described with.
 *
 * Worth its own suite because on this feature the wording *is* the behaviour: the
 * page exists to say what state something is in, and a state rendered as an empty
 * cell, as "Invalid Date", or as two different words on two different screens is
 * the whole failure. None of that is caught by a type — every one of them
 * typechecks perfectly.
 */

import { describe, expect, it } from "vitest";
import type {
  AgentInstance,
  AgentInstanceOperation,
  AgentInstanceState,
} from "@/api";
import {
  NOT_REPORTED,
  autoTitleFrom,
  conversationTitle,
  hasConversationName,
  labelPairs,
  operationAppearance,
  orNotReported,
  relativeAge,
  shortInstanceId,
  stateAppearance,
} from "./instanceLabels";

/** Every member of each union, written out so a new one fails to compile here. */
const ALL_STATES: AgentInstanceState[] = [
  "unspecified",
  "creating",
  "ready",
  "suspended",
  "failed",
  "deleting",
  "deleted",
  "unknown",
];

const ALL_OPERATIONS: AgentInstanceOperation[] = [
  "unspecified",
  "create",
  "suspend",
  "resume",
  "delete",
  "unknown",
];

function instance(overrides: Partial<AgentInstance> = {}): AgentInstance {
  return {
    id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
    namespace: "kagent",
    name: "",
    creator: "alice@example.com",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-18T09:12:00Z",
    updatedAt: "2026-08-20T14:03:00Z",
    labels: {},
    ...overrides,
  };
}

describe("state wording", () => {
  it("has a label and a meaning for every state, including the two zero cases", () => {
    for (const state of ALL_STATES) {
      const appearance = stateAppearance(state);
      expect(appearance.label, `${state} needs a label`).toBeTruthy();
      expect(appearance.meaning, `${state} needs a meaning`).toBeTruthy();
    }
  });

  /*
   * These two are different facts and the page must not merge them. "Not reported"
   * is the controller having sent the enum's zero value; "Unrecognised" is a value
   * this build has no name for, which means the cluster is ahead of the UI. A
   * reader who sees the wrong one of the two goes looking in the wrong place.
   */
  it("distinguishes a state the controller did not send from one it does not know", () => {
    expect(stateAppearance("unspecified").label).toBe(NOT_REPORTED);
    expect(stateAppearance("unknown").label).not.toBe(NOT_REPORTED);
  });

  it("marks only the states that are wrong as wrong", () => {
    expect(stateAppearance("ready").tone).toBe("success");
    expect(stateAppearance("failed").tone).toBe("error");
    // Suspended is somebody's decision, not a fault, so it is not coloured as one.
    expect(stateAppearance("suspended").tone).toBe("default");
  });
});

describe("operation wording", () => {
  it("has a label for every operation", () => {
    for (const operation of ALL_OPERATIONS) {
      expect(operationAppearance(operation).label, `${operation}`).toBeTruthy();
    }
  });

  /*
   * The zero value means the opposite here of what it means for a state: the
   * controller clears `operation` when it finishes, so nothing in flight is the
   * ordinary condition. `inProgress` is what lets the page render it as quiet text
   * rather than as a tag on every healthy row.
   */
  it("treats no operation as ordinary and every named one as in flight", () => {
    expect(operationAppearance("unspecified").inProgress).toBe(false);
    for (const operation of ALL_OPERATIONS.filter((o) => o !== "unspecified")) {
      expect(operationAppearance(operation).inProgress, `${operation}`).toBe(true);
    }
  });
});

describe("absent values", () => {
  it("says a value was not reported rather than rendering nothing", () => {
    expect(orNotReported(undefined)).toBe(NOT_REPORTED);
    expect(orNotReported("")).toBe(NOT_REPORTED);
    // Whitespace counts as absent: proto3 sends "" for an unset string, and a
    // fixture or a controller that pads one would otherwise render a blank cell
    // that looks exactly like a bug.
    expect(orNotReported("   ")).toBe(NOT_REPORTED);
    expect(orNotReported("kagent/k8s-agent")).toBe("kagent/k8s-agent");
  });
});

describe("age", () => {
  it("describes a timestamp in words", () => {
    const anHourAgo = new Date(Date.now() - 60 * 60 * 1000).toISOString();
    expect(relativeAge(anHourAgo)).toMatch(/ago$/);
  });

  /*
   * An unset `google.protobuf.Timestamp` converts to an empty string, and this
   * column used to print "Invalid Date ago" for it — which is worse than a blank,
   * because it looks like data.
   */
  it("says nothing was reported rather than printing an invalid date", () => {
    expect(relativeAge("")).toBe(NOT_REPORTED);
    expect(relativeAge("not a date")).toBe(NOT_REPORTED);
  });
});

describe("identity and labels", () => {
  it("shortens a UUID to something a row can show", () => {
    expect(shortInstanceId("6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44")).toBe("6f1c9d20");
  });

  it("orders labels so the same set always reads the same way", () => {
    const pairs = labelPairs(
      instance({ labels: { tier: "interactive", team: "platform" } }),
    );
    expect(pairs).toEqual(["team=platform", "tier=interactive"]);
  });

  it("has nothing to say about an instance with no labels", () => {
    expect(labelPairs(instance())).toEqual([]);
  });
});

/**
 * What a conversation is called.
 *
 * The single most visible consequence of the model change, and the one that made
 * the old page unreadable: eight rows of hex under a heading that said "Agent". A
 * bare UUID is a database key, and presenting one as though somebody had chosen it
 * is the specific failure these pin.
 */
describe("naming a conversation", () => {
  it("uses the name the reader gave it", () => {
    expect(conversationTitle(instance({ name: "Tuesday cluster review" }))).toBe(
      "Tuesday cluster review",
    );
  });

  it("never renders a bare UUID as if it were a name", () => {
    const title = conversationTitle(instance({ name: "" }));
    // Not the id, and not merely "shorter than the id": the point is that what is on
    // screen reads as a statement about the record rather than as a chosen name.
    expect(title).not.toBe("6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44");
    expect(title).toContain("Untitled");
    // The short id is still there, because two untitled conversations with one agent
    // have nothing else to tell them apart.
    expect(title).toContain("6f1c9d20");
  });

  it("prefers a stored name over a title derived from the transcript", () => {
    // A reader who named a conversation has overruled whatever it opened with, and a
    // derived title winning would silently undo the rename.
    expect(
      conversationTitle(instance({ name: "Renamed" }), "why is checkout down"),
    ).toBe("Renamed");
  });

  it("falls back to a derived title before falling back to the id", () => {
    expect(
      conversationTitle(instance({ name: "" }), "why is checkout down"),
    ).toBe("why is checkout down");
  });

  it("treats a whitespace-only name as no name at all", () => {
    // The controller refuses such a name, so it should not exist — but a row written
    // before the validation, or by another client, still can. Rendering it would be
    // a blank cell that looks like a rendering fault.
    expect(conversationTitle(instance({ name: "   " }))).toContain("Untitled");
    expect(hasConversationName(instance({ name: "   " }))).toBe(false);
    expect(hasConversationName(instance({ name: "Named" }))).toBe(true);
  });
});

describe("deriving a title from the first thing said", () => {
  it("has nothing to offer for an empty or absent message", () => {
    // `undefined` rather than an ellipsis or an empty string, so a caller falls back
    // to the id instead of rendering a title made of nothing.
    expect(autoTitleFrom(undefined)).toBeUndefined();
    expect(autoTitleFrom("   ")).toBeUndefined();
  });

  it("collapses the whitespace a pasted message carries", () => {
    expect(autoTitleFrom("why is\n\n  checkout   down")).toBe("why is checkout down");
  });

  it("cuts on a word boundary rather than mid-word", () => {
    const long =
      "please explain why the checkout deployment has been failing since the weekend release";
    const title = autoTitleFrom(long);
    expect(title?.endsWith("…")).toBe(true);

    const stem = title!.replace("…", "");
    // A prefix of what was said, and one that ends where a word ends: the original
    // continues with a space at exactly that point. A mid-word cut would leave the
    // reader a fragment that reads as a rendering fault rather than as a summary.
    expect(long.startsWith(stem)).toBe(true);
    expect(long[stem.length]).toBe(" ");
  });

  it("still cuts a single long word rather than returning almost nothing", () => {
    const title = autoTitleFrom("a".repeat(120));
    // A word-boundary cut on a string with no boundaries would otherwise leave an
    // ellipsis on its own.
    expect(title).toBe(`${"a".repeat(60)}…`);
  });
});
