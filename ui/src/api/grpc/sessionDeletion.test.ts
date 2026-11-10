import { afterEach, expect, it } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import { SessionService } from "@/generated/kagent/api/v1/sessions_pb";
import { setApiTransport } from "../transport";
import { defaultOperations } from "./operations";

afterEach(() => setApiTransport(undefined));

it.each([undefined, false, true])("passes an explicit force=%s choice to Session deletion", async (force) => {
  const requests: { sessionId: string; force: boolean }[] = [];
  setApiTransport(createRouterTransport((router) => {
    router.service(SessionService, {
      deleteSession(request) {
        requests.push({ sessionId: request.sessionId, force: request.force });
        return {};
      },
    });
  }));
  await defaultOperations["agentInstances.delete"]({ id: "session", force }, {});
  expect(requests).toEqual([{ sessionId: "session", force: force ?? false }]);
});
