import { describe, expect, it } from "vitest";
import type { AgentInstance } from "@/api";
import { isLifecycleBusy, lifecycleReading } from "./lifecycleReading";

/**
 * What the chat page is allowed to say the agent is doing.
 *
 * The value of these is mostly in what they *refuse*. A lifecycle indicator is the
 * easiest thing in a UI to fake — three dots on a timer look convincing and prove
 * nothing — so each case below pins a stage to the field it was read from, and the
 * last two pin the two claims this build cannot make.
 */

function instance(overrides: Partial<AgentInstance> = {}): AgentInstance {
  return {
    id: "instance-1",
    namespace: "kagent",
    // Empty rather than absent, which is what the API returns: the column was added
    // after the table existed, so an unnamed conversation reads back as "" and never
    // as undefined. A fixture that omitted it would let a component get away with
    // treating the two as interchangeable.
    name: "",
    creator: "someone",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-24T10:00:00Z",
    updatedAt: "2026-08-24T10:00:00Z",
    labels: {},
    ...overrides,
  };
}

describe("lifecycleReading", () => {
  it("reads resuming from the operation the controller has claimed", () => {
    const reading = lifecycleReading(instance({ operation: "resume" }), "idle");
    expect(reading.active).toBe("resuming");
    expect(reading.source).toMatch(/claimed a resume operation/);
  });

  it("reads suspending the same way", () => {
    const reading = lifecycleReading(instance({ operation: "suspend" }), "idle");
    expect(reading.active).toBe("suspending");
    expect(reading.source).toMatch(/claimed a suspend operation/);
  });

  it("lets a claimed operation outrank the turn, because the controller is acting on the instance right now", () => {
    // Both are true at once — a turn in flight and a suspend being carried out —
    // and the instance's own lifecycle is the more specific of the two.
    const reading = lifecycleReading(instance({ operation: "suspend" }), "streaming");
    expect(reading.active).toBe("suspending");
  });

  it("reads answering from the turn, and says which part of it it read", () => {
    expect(lifecycleReading(instance(), "sending")).toMatchObject({
      active: "running",
      source: expect.stringMatching(/has not answered yet/),
    });
    expect(lifecycleReading(instance(), "working")).toMatchObject({
      active: "running",
      source: expect.stringMatching(/reports itself as working/),
    });
    expect(lifecycleReading(instance(), "streaming")).toMatchObject({
      active: "running",
      source: expect.stringMatching(/Content is arriving/),
    });
  });

  it("shows no stage at rest, so the indicator is not permanently mid-animation", () => {
    const ready = lifecycleReading(instance(), "idle");
    expect(ready.active).toBeUndefined();
    expect(ready.summary).toBe("Agent is ready");

    const suspended = lifecycleReading(instance({ state: "suspended" }), "idle");
    expect(suspended.active).toBeUndefined();
    expect(suspended.summary).toBe("Agent is suspended");
  });

  it("does not claim a stage for a turn that has finished", () => {
    // The reported symptom this guards against is the opposite of a stall: an
    // indicator that goes on saying the agent is answering after it has answered.
    expect(lifecycleReading(instance(), "completed").active).toBeUndefined();
    expect(lifecycleReading(instance(), "canceled").active).toBeUndefined();
    expect(lifecycleReading(instance(), "failed").active).toBeUndefined();
  });

  it("does NOT invent a suspend after a turn ends", () => {
    /*
     * The thing this file exists to refuse.
     *
     * A substrate agent does suspend itself once it has answered, and it would be
     * easy — and wrong — to animate that stage on a timer when a turn completes.
     * Nothing in the API reports a self-suspend: an instance has no event stream,
     * and an A2A task says nothing about the actor behind it. The stage lights up
     * only for a suspend the controller has claimed on the record.
     */
    const answered = lifecycleReading(
      instance({ state: "ready", operation: "unspecified" }),
      "completed",
    );
    expect(answered.active).toBeUndefined();
    expect(answered.summary).toBe("Agent is ready");
  });

  it("says the agent is waiting for an answer, even when the turn is long over", () => {
    // A parked turn outlives the page that watched it park. After a reload the turn
    // is `idle` and the conversation is still holding a question — so a reading of
    // "Agent is ready" would be wrong about an agent that will refuse the next thing typed.
    const reading = lifecycleReading(instance(), "idle", true);
    expect(reading.summary).toBe("Waiting for your answer");
    expect(reading.source).toMatch(/active-task slot/);
  });

  it("lets a claimed operation outrank a pending question", () => {
    // The controller acting on the instance right now is the more specific fact,
    // and the question is not going anywhere.
    expect(lifecycleReading(instance({ operation: "resume" }), "idle", true).active).toBe(
      "resuming",
    );
  });

  it("says it cannot say, rather than guessing, before the record has loaded", () => {
    const reading = lifecycleReading(undefined, "idle");
    expect(reading.active).toBeUndefined();
    expect(reading.summary).toMatch(/not read yet/);
  });

  it("carries a failure's own message, which is the only place the reason exists", () => {
    const reading = lifecycleReading(
      instance({ state: "failed", failure: { message: "no free workers available" } }),
      "idle",
    );
    expect(reading.summary).toBe("Failed");
    expect(reading.source).toBe("no free workers available");
  });

  it("names a state it does not otherwise handle instead of rendering nothing", () => {
    // A build older than the cluster meets an enum member it has never heard of.
    const reading = lifecycleReading(instance({ state: "unknown" }), "idle");
    expect(reading.summary).toBe("Agent is unknown");
  });
});

describe("isLifecycleBusy", () => {
  it("is true for exactly the readings that have a stage, so polling and showing agree", () => {
    // Two decisions taken separately would drift: a page polling on one rule while
    // the indicator changes on another either misses a stage or reads for nothing.
    expect(isLifecycleBusy(instance({ operation: "resume" }), "idle")).toBe(true);
    expect(isLifecycleBusy(instance(), "streaming")).toBe(true);
    expect(isLifecycleBusy(instance(), "idle")).toBe(false);
    expect(isLifecycleBusy(instance({ state: "suspended" }), "idle")).toBe(false);
    expect(isLifecycleBusy(undefined, "idle")).toBe(false);
    // A conversation waiting on its reader is not the instance doing something:
    // polling it would be a read per second for a state only the reader can change.
    expect(isLifecycleBusy(instance(), "idle", true)).toBe(false);
  });
});
