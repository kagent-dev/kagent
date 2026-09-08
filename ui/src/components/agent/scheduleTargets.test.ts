import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { ScheduledRunSchema } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
import { schedulesFor } from "./scheduleTargets";

const schedule = (id: string, template: string, harness: string, namespace = "kagent") =>
  create(ScheduledRunSchema, {
    id,
    harness: { namespace, name: harness },
    agentTemplate: { namespace, name: template },
  });

describe("schedulesFor", () => {
  const pair = { namespace: "kagent", agentTemplate: "k8s", harness: "fast-lane" };

  it("keeps the schedules that name this pair", () => {
    const mine = schedule("a", "k8s", "fast-lane");
    expect(schedulesFor([mine], pair)).toEqual([mine]);
  });

  it("drops a schedule for the same template on another harness", () => {
    // The case matching on the template alone would get wrong: one template on two
    // harnesses is two agents, each with its own schedules.
    expect(schedulesFor([schedule("b", "k8s", "k8s-agent")], pair)).toEqual([]);
  });

  it("drops another template on this harness, and the same pair elsewhere", () => {
    const rows = [
      schedule("c", "support", "fast-lane"),
      schedule("d", "k8s", "fast-lane", "other"),
    ];
    expect(schedulesFor(rows, pair)).toEqual([]);
  });

  it("drops a schedule with no target refs rather than treating it as a match", () => {
    expect(schedulesFor([create(ScheduledRunSchema, { id: "e" })], pair)).toEqual([]);
  });
});
