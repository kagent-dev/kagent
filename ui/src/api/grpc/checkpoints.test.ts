import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { CheckpointService, CheckpointState } from "@/generated/kagent/api/v1alpha1/checkpoints_pb";
import { ErrorInfoSchema } from "@/generated/google/rpc/error_details_pb";
import { setApiTransport } from "../transport";
import { defaultOperations } from "./operations";

afterEach(() => setApiTransport(undefined));

it("retries only a pending snapshot with the original request and task", async () => {
  const calls: string[][] = [];
  setApiTransport(createRouterTransport((router) => {
    router.service(CheckpointService, {
      createCheckpoint(request) {
        calls.push([request.requestId, request.expectedHeadTaskId]);
        if (calls.length === 1) {
          throw new ConnectError("snapshot pending", Code.FailedPrecondition, undefined, [
            { desc: ErrorInfoSchema, value: { domain: "kagent.dev", reason: "KAGENT_CHECKPOINT_SNAPSHOT_PENDING" } },
          ]);
        }
        return { checkpoint: { id: "checkpoint", agentInstanceId: request.agentInstanceId, headTaskId: request.expectedHeadTaskId, state: CheckpointState.READY } };
      },
    });
  }));
  const result = await defaultOperations["agentInstances.checkpoints.create"]({ id: "instance", requestId: "request", expectedHeadTaskId: "turn-a" }, {});
  expect(result.headTaskId).toBe("turn-a");
  expect(calls).toEqual([["request", "turn-a"], ["request", "turn-a"]]);
});

it.each(["KAGENT_CHECKPOINT_CONVERSATION_ADVANCED", undefined])("does not retry %s", async (reason) => {
  let calls = 0;
  setApiTransport(createRouterTransport((router) => {
    router.service(CheckpointService, {
      createCheckpoint() {
        calls++;
        throw new ConnectError("cannot checkpoint", Code.FailedPrecondition, undefined, reason ? [
          { desc: ErrorInfoSchema, value: { domain: "kagent.dev", reason } },
        ] : []);
      },
    });
  }));
  await expect(defaultOperations["agentInstances.checkpoints.create"]({ id: "instance", requestId: "request", expectedHeadTaskId: "turn-a" }, {})).rejects.toMatchObject({ reason });
  expect(calls).toBe(1);
});
