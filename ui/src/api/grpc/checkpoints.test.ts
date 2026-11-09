import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { CheckpointService, CheckpointState } from "@/generated/kagent/api/v1/checkpoints_pb";
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
        return { checkpoint: { id: "checkpoint", sessionId: request.sessionId, headTaskId: request.expectedHeadTaskId, state: CheckpointState.READY } };
      },
    });
  }));
  const result = await defaultOperations["agentInstances.checkpoints.create"]({ id: "instance", requestId: "request", expectedHeadTaskId: "turn-a" }, {});
  expect(result.agentInstanceId).toBe("instance");
  expect(result.headTaskId).toBe("turn-a");
  expect(calls).toEqual([["request", "turn-a"], ["request", "turn-a"]]);
});

it("forks through Session RPCs and reads the new conversation identity", async () => {
  setApiTransport(createRouterTransport((router) => {
    router.service(CheckpointService, {
      forkSession(request) {
        expect(request).toMatchObject({ checkpointId: "checkpoint", requestId: "fork-request" });
        return { session: { id: "forked-session", contextId: "forked-session", name: "Saved turn" } };
      },
    });
  }));
  const result = await defaultOperations["agentInstances.checkpoints.fork"]({ checkpointId: "checkpoint", requestId: "fork-request" }, {});
  expect(result).toMatchObject({ id: "forked-session", contextId: "forked-session", name: "Saved turn" });
});

it.each(["KAGENT_CHECKPOINT_CONVERSATION_ADVANCED", "KAGENT_CHECKPOINT_FAILED", undefined])("does not retry %s", async (reason) => {
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

it("reads failed cleanup candidates beyond the first page", async () => {
  const tokens: string[] = [];
  setApiTransport(createRouterTransport((router) => {
    router.service(CheckpointService, {
      listCheckpoints(request) {
        tokens.push(request.page?.pageToken ?? "");
        return request.page?.pageToken
          ? { checkpoints: [{ id: "failed", sessionId: request.sessionId, state: CheckpointState.FAILED, failure: { message: "Copy could not be verified" } }], page: {} }
          : { checkpoints: [{ id: "ready", sessionId: request.sessionId, state: CheckpointState.READY }], page: { nextPageToken: "next" } };
      },
    });
  }));
  const result = await defaultOperations["agentInstances.checkpoints.list"]({ id: "instance" }, {});
  expect(tokens).toEqual(["", "next"]);
  expect(result).toEqual(expect.arrayContaining([
    expect.objectContaining({ id: "ready", state: "ready" }),
    expect.objectContaining({ id: "failed", state: "failed", failure: "Copy could not be verified" }),
  ]));
});

it("rejects a repeated snapshot page rather than returning an incomplete list", async () => {
  let calls = 0;
  setApiTransport(createRouterTransport((router) => {
    router.service(CheckpointService, {
      listCheckpoints() {
        calls++;
        return { checkpoints: [], page: { nextPageToken: "same" } };
      },
    });
  }));
  await expect(defaultOperations["agentInstances.checkpoints.list"]({ id: "instance" }, {})).rejects.toThrow("repeated a page");
  expect(calls).toBe(2);
});
