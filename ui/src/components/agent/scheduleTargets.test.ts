import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { ScheduledRunSchema } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
import { schedulesFor } from "./scheduleTargets";

describe("schedulesFor", () => {
  it("matches only the exact Agent namespace and name", () => {
    const mine = create(ScheduledRunSchema, {id:"mine", agent:{namespace:"team", name:"assistant"}});
    const otherAgent = create(ScheduledRunSchema, {id:"other", agent:{namespace:"team", name:"other"}});
    const otherNamespace = create(ScheduledRunSchema, {id:"elsewhere", agent:{namespace:"elsewhere", name:"assistant"}});
    const absent = create(ScheduledRunSchema, {id:"absent"});
    expect(schedulesFor([mine,otherAgent,otherNamespace,absent], {namespace:"team",name:"assistant"})).toEqual([mine]);
  });
});
