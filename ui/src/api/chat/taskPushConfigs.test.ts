import { afterEach, describe, expect, it } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import { A2AService, TaskState } from "@/generated/a2a_pb";
import { setApiTransport } from "../transport";
import { deleteTaskPushConfig, listTaskPushConfigs, saveTaskPushConfig, taskAcceptsPushConfigs } from "./taskPushConfigs";

const conversation = {
  id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
  agent: "team-a/assistant",
};

afterEach(() => setApiTransport(undefined));

describe("task push configurations", () => {
  it("allows edits only while the task is active", async () => {
    let state = TaskState.INPUT_REQUIRED;
    setApiTransport(createRouterTransport(({ service }) => {
      service(A2AService, {
        getTask: async (request) => {
          expect(request.tenant).toBe(conversation.agent);
          expect(request.id).toBe("task-1");
          return { id: request.id, status: { state } };
        },
      });
    }));
    expect(await taskAcceptsPushConfigs(conversation, "task-1")).toBe(true);
    state = TaskState.COMPLETED;
    expect(await taskAcceptsPushConfigs(conversation, "task-1")).toBe(false);
  });

  it("pages within a task and sends its Agent route on writes", async () => {
    const pageTokens: string[] = [];
    const writes: string[] = [];
    setApiTransport(createRouterTransport(({ service }) => {
      service(A2AService, {
        listTaskPushNotificationConfigs: async (request) => {
          expect(request.tenant).toBe(conversation.agent);
          expect(request.taskId).toBe("task-1");
          expect(request.pageSize).toBe(100);
          pageTokens.push(request.pageToken);
          return request.pageToken
            ? { configs: [{ id: "second", url: "https://two.example/callback", taskId: "task-1" }] }
            : { configs: [{ id: "first", url: "https://one.example/callback", taskId: "task-1" }], nextPageToken: "next" };
        },
        createTaskPushNotificationConfig: async (request) => {
          expect(request.tenant).toBe(conversation.agent);
          writes.push(`create:${request.taskId}:${request.id}:${request.url}`);
          return request;
        },
        deleteTaskPushNotificationConfig: async (request) => {
          expect(request.tenant).toBe(conversation.agent);
          writes.push(`delete:${request.taskId}:${request.id}`);
          return {};
        },
      });
    }));

    expect(await listTaskPushConfigs(conversation, "task-1")).toEqual([
      { id: "first", url: "https://one.example/callback" },
      { id: "second", url: "https://two.example/callback" },
    ]);
    expect(pageTokens).toEqual(["", "next"]);
    expect(await saveTaskPushConfig(conversation, "task-1", { id: "named", url: "https://new.example/callback" }))
      .toEqual({ id: "named", url: "https://new.example/callback" });
    await deleteTaskPushConfig(conversation, "task-1", "named");
    expect(writes).toEqual([
      "create:task-1:named:https://new.example/callback",
      "delete:task-1:named",
    ]);
  });

  it("stops when a server repeats a page token", async () => {
    setApiTransport(createRouterTransport(({ service }) => {
      service(A2AService, {
        listTaskPushNotificationConfigs: async () => ({ configs: [], nextPageToken: "same" }),
      });
    }));

    await expect(listTaskPushConfigs(conversation, "task-1")).rejects.toThrow("repeated a push configuration page token");
  });

  it("creates a callback without client secrets", async () => {
    setApiTransport(createRouterTransport(({ service }) => {
      service(A2AService, {
        createTaskPushNotificationConfig: async (request) => {
          expect(request.token).toBe("");
          expect(request.authentication).toBeUndefined();
          return request;
        },
        listTaskPushNotificationConfigs: async () => ({
          configs: [{ id: "named", taskId: "task-1", url: "https://receiver.example/callback" }],
        }),
      });
    }));

    const config = { id: "named", url: "https://receiver.example/callback" };
    expect(await saveTaskPushConfig(conversation, "task-1", config)).toEqual({ id: config.id, url: config.url });
    expect(await listTaskPushConfigs(conversation, "task-1")).toEqual([{ id: config.id, url: config.url }]);
  });
});
